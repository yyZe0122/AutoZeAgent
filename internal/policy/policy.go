// Package policy contains the mandatory, code-enforced risk policy used before
// any tool or optional module can request a side effect.
package policy

import (
	"errors"
	"fmt"
	"strings"
)

type RiskLevel string

const (
	RiskR0 RiskLevel = "R0" // Pure local reads.
	RiskR1 RiskLevel = "R1" // Reversible workspace writes.
	RiskR2 RiskLevel = "R2" // Process execution and network reads.
	RiskR3 RiskLevel = "R3" // External writes, messages, and git push.
	RiskR4 RiskLevel = "R4" // Deletion, system changes, and permission changes.
)

var riskOrder = map[RiskLevel]int{
	RiskR0: 0,
	RiskR1: 1,
	RiskR2: 2,
	RiskR3: 3,
	RiskR4: 4,
}

func (r RiskLevel) Valid() bool {
	_, ok := riskOrder[r]
	return ok
}

// AtLeast reports whether actual is at least as restrictive as required.
// Unknown levels fail closed.
func AtLeast(actual, required RiskLevel) bool {
	actualOrder, actualOK := riskOrder[actual]
	requiredOrder, requiredOK := riskOrder[required]
	return actualOK && requiredOK && actualOrder >= requiredOrder
}

// AtMost reports whether actual is no more severe than max.
// Unknown levels fail closed.
func AtMost(actual, max RiskLevel) bool {
	actualOrder, actualOK := riskOrder[actual]
	maxOrder, maxOK := riskOrder[max]
	return actualOK && maxOK && actualOrder <= maxOrder
}

func (r RiskLevel) Description() string {
	switch r {
	case RiskR0:
		return "pure local read"
	case RiskR1:
		return "reversible workspace write"
	case RiskR2:
		return "process execution or network read"
	case RiskR3:
		return "external write, message send, or git push"
	case RiskR4:
		return "deletion, system change, or permission change"
	default:
		return "unknown risk"
	}
}

type Action string

const (
	ActionAllow           Action = "allow"
	ActionRequireApproval Action = "require_approval"
	ActionDeny            Action = "deny"
)

func (a Action) Valid() bool {
	return a == ActionAllow || a == ActionRequireApproval || a == ActionDeny
}

// Config is deliberately explicit. Any absent or invalid rule is denied by the
// evaluator instead of inheriting a more permissive fallback.
type Config struct {
	Rules map[RiskLevel]Action `json:"rules"`
}

// DefaultConfig permits pure local reads and requires explicit approval for
// every known side-effect level.
func DefaultConfig() Config {
	return Config{Rules: map[RiskLevel]Action{
		RiskR0: ActionAllow,
		RiskR1: ActionRequireApproval,
		RiskR2: ActionRequireApproval,
		RiskR3: ActionRequireApproval,
		RiskR4: ActionRequireApproval,
	}}
}

type Evaluator struct {
	rules map[RiskLevel]Action
}

func NewEvaluator(config Config) *Evaluator {
	rules := make(map[RiskLevel]Action, len(config.Rules))
	for level, action := range config.Rules {
		rules[level] = action
	}
	return &Evaluator{rules: rules}
}

type Result struct {
	Risk             RiskLevel `json:"risk"`
	Action           Action    `json:"action"`
	Allowed          bool      `json:"allowed"`
	RequiresApproval bool      `json:"requires_approval"`
	Reason           string    `json:"reason"`
}

func (e *Evaluator) Evaluate(level RiskLevel) Result {
	if !level.Valid() {
		return denied(level, "unknown risk level")
	}
	if e == nil {
		return denied(level, "policy evaluator is unavailable")
	}
	action, ok := e.rules[level]
	if !ok || !action.Valid() {
		return denied(level, "policy rule is missing or invalid")
	}
	switch action {
	case ActionAllow:
		return Result{Risk: level, Action: action, Allowed: true, Reason: "policy allows this risk level"}
	case ActionRequireApproval:
		return Result{Risk: level, Action: action, RequiresApproval: true, Reason: "explicit approval is required"}
	default:
		return denied(level, "policy denies this risk level")
	}
}

var (
	ErrDenied           = errors.New("policy denied")
	ErrApprovalRequired = errors.New("approval required")
)

// Authorize combines the configured policy with an independently verified
// approval result. Callers cannot turn a deny rule into an approval request.
func (e *Evaluator) Authorize(level RiskLevel, approved bool) error {
	result := e.Evaluate(level)
	if result.Allowed {
		return nil
	}
	if result.RequiresApproval {
		if approved {
			return nil
		}
		return fmt.Errorf("%w: %s", ErrApprovalRequired, level)
	}
	return fmt.Errorf("%w: %s: %s", ErrDenied, strings.TrimSpace(string(level)), result.Reason)
}

func denied(level RiskLevel, reason string) Result {
	return Result{Risk: level, Action: ActionDeny, Reason: reason}
}
