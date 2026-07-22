package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"autozeagent.local/autozeagent/internal/approval"
	"autozeagent.local/autozeagent/internal/approvalsubmission"
	"autozeagent.local/autozeagent/internal/corequery"
	"autozeagent.local/autozeagent/internal/gateway"
	"autozeagent.local/autozeagent/internal/kernel"
	"autozeagent.local/autozeagent/internal/platform/paths"
	"autozeagent.local/autozeagent/internal/runexecution"
)

const (
	commandTimeout  = 30 * time.Second
	workflowTimeout = 30 * time.Minute
	pollInterval    = 500 * time.Millisecond
)

type taskSubmissionRequest struct {
	PlanID    kernel.PlanID `json:"plan_id"`
	Title     string        `json:"title"`
	Objective string        `json:"objective"`
}

type taskSubmissionResponse struct {
	Task            corequery.Task         `json:"task"`
	Plan            *approval.PlanDocument `json:"plan,omitempty"`
	PlanningPending bool                   `json:"planning_pending,omitempty"`
}

type approvalDecisionRequest struct {
	PlanID       kernel.PlanID             `json:"plan_id"`
	PlanRevision uint64                    `json:"plan_revision"`
	PlanHash     string                    `json:"plan_hash"`
	StepID       kernel.StepID             `json:"step_id,omitempty"`
	Action       approvalsubmission.Action `json:"action"`
	DecidedBy    string                    `json:"decided_by"`
	Reason       string                    `json:"reason"`
}

type runStartRequest struct {
	TaskID       kernel.TaskID `json:"task_id"`
	PlanID       kernel.PlanID `json:"plan_id"`
	PlanRevision uint64        `json:"plan_revision"`
	PlanHash     string        `json:"plan_hash"`
}

func runWorkflow(args []string) error {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	modeValue := flags.String("mode", string(paths.ModeUser), "runtime mode: user or system")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 || strings.TrimSpace(flags.Arg(0)) == "" {
		return errors.New("use autozeagent run [--mode user|system] \"task objective\"")
	}
	mode, err := paths.ParseMode(*modeValue)
	if err != nil {
		return err
	}
	client, err := newWorkflowClient(mode)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), workflowTimeout)
	defer cancel()

	objective := strings.TrimSpace(flags.Arg(0))
	planID, err := randomWorkflowID("plan-")
	if err != nil {
		return err
	}
	var submitted taskSubmissionResponse
	if err := client.DoJSON(ctx, http.MethodPost, "/v1/tasks", taskSubmissionRequest{
		PlanID: kernel.PlanID(planID), Title: taskTitle(objective), Objective: objective,
	}, &submitted); err != nil {
		return fmt.Errorf("create task: %w", err)
	}
	if submitted.Task.ID == "" {
		return errors.New("gateway returned an empty task ID")
	}
	fmt.Printf("Task: %s (%s)\n", submitted.Task.ID, submitted.Task.State)

	if submitted.Plan == nil {
		if !submitted.PlanningPending && submitted.Task.State == string(kernel.TaskCreated) {
			return errors.New("planner is not configured in autozeagentd")
		}
		if err := waitForPlanning(ctx, client, submitted.Task.ID, kernel.PlanID(planID)); err != nil {
			return err
		}
	}
	prompt, err := loadApprovalPrompt(ctx, client, kernel.PlanID(planID), "")
	if err != nil {
		return err
	}
	renderApprovalPrompt(prompt)

	allowed, err := confirmApproval()
	if err != nil {
		return err
	}
	action := approvalsubmission.ActionReject
	if allowed {
		action = approvalsubmission.ActionAllowPlan
	}
	decision, err := decideApproval(ctx, client, prompt, "", action, "local-user", "")
	if err != nil {
		return err
	}
	fmt.Printf("Approval: %s (%s)\n", decision.ID, decision.Decision)
	if !allowed {
		fmt.Println("Plan rejected; no Run was started.")
		return nil
	}

	var started runexecution.StartResult
	if err := client.DoJSON(ctx, http.MethodPost, "/v1/runs", runStartRequest{
		TaskID: prompt.TaskID, PlanID: prompt.PlanID, PlanRevision: prompt.Revision, PlanHash: prompt.PlanHash,
	}, &started); err != nil {
		return fmt.Errorf("start run: %w", err)
	}
	if len(started.RunIDs) == 0 {
		return errors.New("gateway accepted the plan but returned no Run IDs")
	}
	fmt.Printf("Runs accepted: %d\n", len(started.RunIDs))
	runs, err := waitForRuns(ctx, client, started.RunIDs)
	if err != nil {
		return err
	}
	printRunResults(runs)
	return nil
}

func runTaskStatus(args []string) error {
	if len(args) == 0 {
		return errors.New("use autozeagent task status <task-id> [--mode user|system]")
	}
	taskID := strings.TrimSpace(args[0])
	flags := flag.NewFlagSet("task status", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	modeValue := flags.String("mode", string(paths.ModeUser), "runtime mode: user or system")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if taskID == "" || flags.NArg() != 0 {
		return errors.New("use autozeagent task status <task-id> [--mode user|system]")
	}
	mode, err := paths.ParseMode(*modeValue)
	if err != nil {
		return err
	}
	client, err := newWorkflowClient(mode)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	var task corequery.Task
	if err := client.DoJSON(ctx, http.MethodGet, "/v1/tasks/"+url.PathEscape(taskID), nil, &task); err != nil {
		return fmt.Errorf("get task: %w", err)
	}
	return writeJSON(task)
}

func runTaskAction(action string, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("use autozeagent task %s <task-id> [--reason <text>] [--mode user|system]", action)
	}
	taskID := strings.TrimSpace(args[0])
	flags := flag.NewFlagSet("task "+action, flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	modeValue := flags.String("mode", string(paths.ModeUser), "runtime mode: user or system")
	reason := flags.String("reason", "", "reason recorded with the task state change")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if taskID == "" || flags.NArg() != 0 {
		return fmt.Errorf("use autozeagent task %s <task-id> [--reason <text>] [--mode user|system]", action)
	}
	mode, err := paths.ParseMode(*modeValue)
	if err != nil {
		return err
	}
	client, err := newWorkflowClient(mode)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	var current corequery.Task
	path := "/v1/tasks/" + url.PathEscape(taskID)
	if err := client.DoJSON(ctx, http.MethodGet, path, nil, &current); err != nil {
		return fmt.Errorf("get task before %s: %w", action, err)
	}
	var updated corequery.Task
	if err := client.DoJSON(ctx, http.MethodPost, path+"/actions", runexecution.TaskActionRequest{
		ExpectedVersion: current.Version,
		Action:          runexecution.TaskAction(action),
		Reason:          strings.TrimSpace(*reason),
	}, &updated); err != nil {
		return fmt.Errorf("%s task: %w", action, err)
	}
	return writeJSON(updated)
}

func runApprovalShow(args []string) error {
	planID, flags, err := parseApprovalFlags("approval show", args, false)
	if err != nil {
		return err
	}
	mode, err := paths.ParseMode(flags.mode)
	if err != nil {
		return err
	}
	client, err := newWorkflowClient(mode)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	prompt, err := loadApprovalPrompt(ctx, client, kernel.PlanID(planID), kernel.StepID(flags.stepID))
	if err != nil {
		return err
	}
	renderApprovalPrompt(prompt)
	return nil
}

func runApprovalDecide(args []string) error {
	planID, values, err := parseApprovalFlags("approval decide", args, true)
	if err != nil {
		return err
	}
	action := approvalsubmission.Action(values.action)
	if !validApprovalAction(action) {
		return fmt.Errorf("invalid approval action %q", values.action)
	}
	mode, err := paths.ParseMode(values.mode)
	if err != nil {
		return err
	}
	client, err := newWorkflowClient(mode)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	prompt, err := loadApprovalPrompt(ctx, client, kernel.PlanID(planID), kernel.StepID(values.stepID))
	if err != nil {
		return err
	}
	if !promptAllows(prompt, action, kernel.StepID(values.stepID)) {
		return fmt.Errorf("action %q is not available for the selected approval scope", action)
	}
	decision, err := decideApproval(ctx, client, prompt, kernel.StepID(values.stepID), action, values.decidedBy, values.reason)
	if err != nil {
		return err
	}
	return writeJSON(decision)
}

type approvalFlagValues struct {
	mode      string
	stepID    string
	action    string
	decidedBy string
	reason    string
}

func parseApprovalFlags(command string, args []string, requireAction bool) (string, approvalFlagValues, error) {
	if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
		return "", approvalFlagValues{}, fmt.Errorf("use autozeagent %s <plan-id> [options]", command)
	}
	values := approvalFlagValues{}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	flags.StringVar(&values.mode, "mode", string(paths.ModeUser), "runtime mode: user or system")
	flags.StringVar(&values.stepID, "step", "", "limit approval to one plan step")
	if requireAction {
		flags.StringVar(&values.action, "action", "", "allow_plan, allow_once, allow_limited, reject, or request_changes")
		flags.StringVar(&values.decidedBy, "decided-by", "local-user", "approval decision maker")
		flags.StringVar(&values.reason, "reason", "", "approval decision reason")
	}
	if err := flags.Parse(args[1:]); err != nil {
		return "", approvalFlagValues{}, err
	}
	if flags.NArg() != 0 || (requireAction && strings.TrimSpace(values.action) == "") {
		return "", approvalFlagValues{}, fmt.Errorf("use autozeagent %s <plan-id> [options]", command)
	}
	return strings.TrimSpace(args[0]), values, nil
}

func newWorkflowClient(mode paths.Mode) (*gateway.Client, error) {
	layout, err := paths.Resolve(mode)
	if err != nil {
		return nil, err
	}
	client, err := gateway.NewLocalClient(layout.RuntimeDir)
	if err != nil {
		return nil, fmt.Errorf("connect to local gateway: %w", err)
	}
	return client, nil
}

func waitForPlanning(ctx context.Context, client *gateway.Client, taskID kernel.TaskID, planID kernel.PlanID) error {
	lastState := ""
	for {
		var task corequery.Task
		if err := client.DoJSON(ctx, http.MethodGet, "/v1/tasks/"+url.PathEscape(string(taskID)), nil, &task); err != nil {
			return fmt.Errorf("poll task planning: %w", err)
		}
		if task.State != lastState {
			fmt.Printf("Planning state: %s\n", task.State)
			lastState = task.State
		}
		switch task.State {
		case string(kernel.TaskWaitingApproval):
			var plan corequery.Plan
			if err := client.DoJSON(ctx, http.MethodGet, "/v1/plans/"+url.PathEscape(string(planID)), nil, &plan); err != nil {
				return fmt.Errorf("load planned task: %w", err)
			}
			return nil
		case string(kernel.TaskFailed), string(kernel.TaskCancelled):
			return fmt.Errorf("planning ended with task state %s", task.State)
		case string(kernel.TaskCreated):
			return errors.New("planner is not configured in autozeagentd")
		}
		if err := sleepContext(ctx, pollInterval); err != nil {
			return fmt.Errorf("wait for planning: %w", err)
		}
	}
}

func loadApprovalPrompt(ctx context.Context, client *gateway.Client, planID kernel.PlanID, stepID kernel.StepID) (approvalsubmission.Prompt, error) {
	path := "/v1/approvals/prompt?plan_id=" + url.QueryEscape(string(planID))
	if stepID != "" {
		path += "&step_id=" + url.QueryEscape(string(stepID))
	}
	var prompt approvalsubmission.Prompt
	if err := client.DoJSON(ctx, http.MethodGet, path, nil, &prompt); err != nil {
		return approvalsubmission.Prompt{}, fmt.Errorf("load approval prompt: %w", err)
	}
	return prompt, nil
}

func decideApproval(ctx context.Context, client *gateway.Client, prompt approvalsubmission.Prompt, stepID kernel.StepID, action approvalsubmission.Action, decidedBy, reason string) (corequery.Approval, error) {
	var decision corequery.Approval
	if err := client.DoJSON(ctx, http.MethodPost, "/v1/approvals", approvalDecisionRequest{
		PlanID: prompt.PlanID, PlanRevision: prompt.Revision, PlanHash: prompt.PlanHash,
		StepID: stepID, Action: action, DecidedBy: strings.TrimSpace(decidedBy), Reason: strings.TrimSpace(reason),
	}, &decision); err != nil {
		return corequery.Approval{}, fmt.Errorf("submit approval decision: %w", err)
	}
	return decision, nil
}

func waitForRuns(ctx context.Context, client *gateway.Client, runIDs []kernel.RunID) ([]corequery.Run, error) {
	results := make([]corequery.Run, len(runIDs))
	states := make(map[kernel.RunID]string, len(runIDs))
	remaining := len(runIDs)
	for remaining > 0 {
		for index, runID := range runIDs {
			if results[index].State == string(kernel.RunCompleted) {
				continue
			}
			var run corequery.Run
			if err := client.DoJSON(ctx, http.MethodGet, "/v1/runs/"+url.PathEscape(string(runID)), nil, &run); err != nil {
				return nil, fmt.Errorf("poll run %s: %w", runID, err)
			}
			if states[runID] != run.State {
				fmt.Printf("Run %s: %s\n", runID, run.State)
				states[runID] = run.State
			}
			results[index] = run
			switch run.State {
			case string(kernel.RunCompleted):
				remaining--
			case string(kernel.RunFailed), string(kernel.RunCancelled):
				if run.Error != nil && strings.TrimSpace(*run.Error) != "" {
					return nil, fmt.Errorf("run %s %s: %s", run.ID, run.State, *run.Error)
				}
				return nil, fmt.Errorf("run %s ended with state %s", run.ID, run.State)
			}
		}
		if remaining > 0 {
			if err := sleepContext(ctx, pollInterval); err != nil {
				return nil, fmt.Errorf("wait for runs: %w", err)
			}
		}
	}
	return results, nil
}

func renderApprovalPrompt(prompt approvalsubmission.Prompt) {
	fmt.Println()
	fmt.Printf("Plan: %s revision=%d\n", prompt.PlanID, prompt.Revision)
	fmt.Printf("Objective: %s\n", prompt.Objective)
	fmt.Printf("Budget: tokens=%d cost_micros=%d duration_ms=%d\n", prompt.Budget.MaxTokens, prompt.Budget.MaxCostMicros, prompt.Budget.MaxDurationMillis)
	for _, step := range prompt.Steps {
		fmt.Println()
		fmt.Printf("Step %d: %s\n", step.Position+1, step.Title)
		fmt.Printf("  id: %s\n", step.StepID)
		fmt.Printf("  risk: %s\n", step.Risk)
		fmt.Printf("  side effects: %s\n", displayList(step.ExpectedSideEffects))
		fmt.Printf("  rollback: %s\n", step.Rollback)
		fmt.Printf("  timeout_ms: %d\n", step.TimeoutMillis)
		if len(step.Capabilities) == 0 {
			fmt.Println("  capabilities: none")
			continue
		}
		fmt.Println("  capabilities:")
		for _, capability := range step.Capabilities {
			fmt.Printf("    - %s calls=%d one_time=%t duration_ms=%d\n", capability.Tool, capability.MaxCalls, capability.OneTime, capability.MaxDurationMillis)
			if len(capability.Paths) > 0 {
				fmt.Printf("      paths: %s\n", strings.Join(capability.Paths, ", "))
			}
			if capability.Command != "" {
				fmt.Printf("      command: %s\n", capability.Command)
			}
			if len(capability.Arguments) > 0 {
				fmt.Printf("      arguments: %s\n", strings.Join(capability.Arguments, " "))
			}
			if len(capability.NetworkDomains) > 0 {
				fmt.Printf("      domains: %s\n", strings.Join(capability.NetworkDomains, ", "))
			}
		}
	}
	fmt.Println()
}

func confirmApproval() (bool, error) {
	fmt.Print("Allow this Plan? [y/N]: ")
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && len(line) == 0 {
		return false, err
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes", nil
}

func promptAllows(prompt approvalsubmission.Prompt, action approvalsubmission.Action, stepID kernel.StepID) bool {
	for _, option := range prompt.Actions {
		if option.Action == action && option.StepID == stepID {
			return true
		}
	}
	return false
}

func validApprovalAction(action approvalsubmission.Action) bool {
	switch action {
	case approvalsubmission.ActionAllowOnce, approvalsubmission.ActionAllowLimited,
		approvalsubmission.ActionAllowPlan, approvalsubmission.ActionReject, approvalsubmission.ActionRequestChanges:
		return true
	default:
		return false
	}
}

func printRunResults(runs []corequery.Run) {
	fmt.Println()
	fmt.Println("Results:")
	for _, run := range runs {
		step := "plan"
		if run.StepID != nil {
			step = string(*run.StepID)
		}
		fmt.Printf("\n[%s]\n", step)
		if run.Result == nil || strings.TrimSpace(*run.Result) == "" {
			fmt.Println("(no assistant result)")
			continue
		}
		fmt.Println(*run.Result)
	}
}

func displayList(values []string) string {
	if len(values) == 0 {
		return "none"
	}
	return strings.Join(values, "; ")
}

func taskTitle(objective string) string {
	runes := []rune(strings.TrimSpace(objective))
	if len(runes) <= 80 {
		return string(runes)
	}
	return string(runes[:80]) + "…"
}

func randomWorkflowID(prefix string) (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate workflow ID: %w", err)
	}
	return prefix + hex.EncodeToString(value), nil
}

func sleepContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
