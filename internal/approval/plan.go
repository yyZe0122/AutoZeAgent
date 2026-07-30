package approval

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"

	"autozeagent.local/autozeagent/internal/kernel"
	"autozeagent.local/autozeagent/internal/policy"
)

var ErrInvalidPlan = errors.New("invalid plan")

// PlanDocument is the complete scope presented for approval. It intentionally
// uses map-free structures so its canonical JSON is stable and reviewable.
type PlanDocument struct {
	PlanID    kernel.PlanID `json:"plan_id"`
	TaskID    kernel.TaskID `json:"task_id"`
	Revision  uint64        `json:"revision"`
	Objective string        `json:"objective"`
	Budget    PlanBudget    `json:"budget"`
	Steps     []StepScope   `json:"steps"`
}

type PlanBudget struct {
	MaxTokens         int64 `json:"max_tokens"`
	MaxCostMicros     int64 `json:"max_cost_micros"`
	MaxDurationMillis int64 `json:"max_duration_ms"`
}

type StepScope struct {
	StepID              kernel.StepID     `json:"step_id"`
	Position            int               `json:"position"`
	Title               string            `json:"title"`
	Risk                policy.RiskLevel  `json:"risk"`
	ExpectedSideEffects []string          `json:"expected_side_effects"`
	Rollback            string            `json:"rollback"`
	TimeoutMillis       int64             `json:"timeout_ms"`
	Capabilities        []CapabilityScope `json:"capabilities"`
}

// CapabilityScope is the maximum authority a Plan requests for one tool
// capability. Empty fields mean that type of resource is not authorized.
type CapabilityScope struct {
	Capability        string   `json:"capability"`
	Paths             []string `json:"paths"`
	Command           string   `json:"command,omitempty"`
	Arguments         []string `json:"arguments"`
	NetworkDomains    []string `json:"network_domains"`
	MaxDurationMillis int64    `json:"max_duration_ms"`
	MaxCalls          uint64   `json:"max_calls"`
	OneTime           bool     `json:"one_time"`
}

func (p PlanDocument) CanonicalJSON() ([]byte, error) {
	normalized, err := p.normalized()
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return nil, fmt.Errorf("marshal canonical plan: %w", err)
	}
	return encoded, nil
}

func (p PlanDocument) Hash() (string, error) {
	encoded, err := p.CanonicalJSON()
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func (p PlanDocument) normalized() (PlanDocument, error) {
	normalized := PlanDocument{
		PlanID:    p.PlanID,
		TaskID:    p.TaskID,
		Revision:  p.Revision,
		Objective: strings.TrimSpace(p.Objective),
		Budget:    p.Budget,
		Steps:     make([]StepScope, len(p.Steps)),
	}
	if strings.TrimSpace(string(normalized.PlanID)) == "" || strings.TrimSpace(string(normalized.TaskID)) == "" {
		return PlanDocument{}, fmt.Errorf("%w: plan and task IDs are required", ErrInvalidPlan)
	}
	if normalized.Revision == 0 || normalized.Objective == "" {
		return PlanDocument{}, fmt.Errorf("%w: revision and objective are required", ErrInvalidPlan)
	}
	if len(p.Steps) == 0 {
		return PlanDocument{}, fmt.Errorf("%w: at least one step is required", ErrInvalidPlan)
	}

	stepIDs := make(map[kernel.StepID]struct{}, len(p.Steps))
	positions := make(map[int]struct{}, len(p.Steps))
	for i, step := range p.Steps {
		normalizedStep, err := normalizeStep(step)
		if err != nil {
			return PlanDocument{}, err
		}
		if _, exists := stepIDs[normalizedStep.StepID]; exists {
			return PlanDocument{}, fmt.Errorf("%w: duplicate step ID %s", ErrInvalidPlan, normalizedStep.StepID)
		}
		if _, exists := positions[normalizedStep.Position]; exists {
			return PlanDocument{}, fmt.Errorf("%w: duplicate step position %d", ErrInvalidPlan, normalizedStep.Position)
		}
		stepIDs[normalizedStep.StepID] = struct{}{}
		positions[normalizedStep.Position] = struct{}{}
		normalized.Steps[i] = normalizedStep
	}
	sort.Slice(normalized.Steps, func(i, j int) bool {
		return normalized.Steps[i].Position < normalized.Steps[j].Position
	})
	return normalized, nil
}

func normalizeStep(step StepScope) (StepScope, error) {
	normalized := StepScope{
		StepID:              step.StepID,
		Position:            step.Position,
		Title:               strings.TrimSpace(step.Title),
		Risk:                step.Risk,
		ExpectedSideEffects: normalizeTextList(step.ExpectedSideEffects),
		Rollback:            strings.TrimSpace(step.Rollback),
		TimeoutMillis:       step.TimeoutMillis,
		Capabilities:        make([]CapabilityScope, len(step.Capabilities)),
	}
	if strings.TrimSpace(string(normalized.StepID)) == "" || normalized.Position < 0 || normalized.Title == "" {
		return StepScope{}, fmt.Errorf("%w: step ID, non-negative position, and title are required", ErrInvalidPlan)
	}
	if !normalized.Risk.Valid() {
		return StepScope{}, fmt.Errorf("%w: invalid risk level %q", ErrInvalidPlan, normalized.Risk)
	}
	for i, capability := range step.Capabilities {
		value, err := normalizeCapability(capability)
		if err != nil {
			return StepScope{}, fmt.Errorf("step %s: %w", step.StepID, err)
		}
		normalized.Capabilities[i] = value
	}
	sort.Slice(normalized.Capabilities, func(i, j int) bool {
		left, _ := json.Marshal(normalized.Capabilities[i])
		right, _ := json.Marshal(normalized.Capabilities[j])
		return string(left) < string(right)
	})
	return normalized, nil
}

// NormalizeCapabilityForPlan normalizes and validates one capability scope for plan documents.
func NormalizeCapabilityForPlan(scope CapabilityScope) (CapabilityScope, error) {
	return normalizeCapability(scope)
}

func normalizeCapability(scope CapabilityScope) (CapabilityScope, error) {
	args := scope.Arguments
	if args == nil {
		args = []string{}
	}
	// Always non-nil so grant command matching (slices.Equal) treats null JSON as [].
	copiedArgs := make([]string, len(args))
	copy(copiedArgs, args)
	normalized := CapabilityScope{
		Capability:        strings.TrimSpace(scope.Capability),
		Paths:             normalizePaths(scope.Paths),
		Command:           strings.TrimSpace(scope.Command),
		Arguments:         copiedArgs,
		NetworkDomains:    normalizeDomains(scope.NetworkDomains),
		MaxDurationMillis: scope.MaxDurationMillis,
		MaxCalls:          scope.MaxCalls,
		OneTime:           scope.OneTime,
	}
	if normalized.Capability == "" {
		return CapabilityScope{}, fmt.Errorf("%w: capability name is required", ErrInvalidPlan)
	}
	if normalized.MaxDurationMillis <= 0 {
		return CapabilityScope{}, fmt.Errorf("%w: maximum duration must be positive", ErrInvalidPlan)
	}
	if normalized.OneTime {
		if normalized.MaxCalls == 0 || normalized.MaxCalls > 1 {
			return CapabilityScope{}, fmt.Errorf("%w: one-time scope must allow exactly one call", ErrInvalidPlan)
		}
	} else if normalized.MaxCalls == 0 {
		return CapabilityScope{}, fmt.Errorf("%w: maximum calls must be positive", ErrInvalidPlan)
	}
	if normalized.Capability == "process_exec" {
		if normalized.Command == "" {
			return CapabilityScope{}, fmt.Errorf("%w: process_exec requires command", ErrInvalidPlan)
		}
		if len(normalized.Paths) == 0 {
			return CapabilityScope{}, fmt.Errorf("%w: process_exec requires at least one absolute working-directory path", ErrInvalidPlan)
		}
		for _, p := range normalized.Paths {
			if !isAbsoluteCapabilityPath(p) {
				return CapabilityScope{}, fmt.Errorf("%w: process_exec path %q must be absolute", ErrInvalidPlan, p)
			}
		}
	}
	return normalized, nil
}

func isAbsoluteCapabilityPath(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	// Canonical plan paths use slash form after normalizePaths.
	if path.IsAbs(value) || strings.HasPrefix(value, "/") {
		return true
	}
	// Windows drive path retained as C:/... after slash normalization.
	return len(value) >= 3 && value[1] == ':' && (value[2] == '/' || value[2] == '\\')
}

func normalizeTextList(values []string) []string {
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			normalized = append(normalized, value)
		}
	}
	return sortedUnique(normalized)
}

func normalizePaths(values []string) []string {
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(strings.ReplaceAll(value, `\`, "/"))
		if value == "" {
			continue
		}
		normalized = append(normalized, path.Clean(value))
	}
	return sortedUnique(normalized)
}

func normalizeDomains(values []string) []string {
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(value), "."))
		if value != "" {
			normalized = append(normalized, value)
		}
	}
	return sortedUnique(normalized)
}

func sortedUnique(values []string) []string {
	sort.Strings(values)
	if len(values) == 0 {
		return []string{}
	}
	result := values[:1]
	for _, value := range values[1:] {
		if value != result[len(result)-1] {
			result = append(result, value)
		}
	}
	return result
}
