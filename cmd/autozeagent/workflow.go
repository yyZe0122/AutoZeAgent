package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"autozeagent.local/autozeagent/internal/approvalsubmission"
	"autozeagent.local/autozeagent/internal/corequery"
	"autozeagent.local/autozeagent/internal/gatewayclient"
	"autozeagent.local/autozeagent/internal/kernel"
	"autozeagent.local/autozeagent/internal/platform/paths"
	"autozeagent.local/autozeagent/internal/runexecution"
)

const (
	commandTimeout  = 30 * time.Second
	workflowTimeout = 30 * time.Minute
	pollInterval    = 500 * time.Millisecond
)

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
	if err := ensureDaemon(mode); err != nil {
		return err
	}
	client, err := gatewayclient.New(mode)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), workflowTimeout)
	defer cancel()

	objective := strings.TrimSpace(flags.Arg(0))
	submitted, err := client.SubmitTask(ctx, gatewayclient.TaskSubmissionRequest{
		Title: gatewayclient.TaskTitle(objective), Objective: objective,
	})
	if err != nil {
		return err
	}
	if submitted.Task.ID == "" {
		return errors.New("gateway returned an empty task ID")
	}
	fmt.Printf("Task: %s (%s)\n", submitted.Task.ID, submitted.Task.State)

	if submitted.Plan == nil {
		if !submitted.PlanningPending && submitted.Task.State == string(kernel.TaskCreated) {
			return errors.New("planner is not configured in autozeagentd")
		}
		if err := waitForPlanning(ctx, client, submitted.Task.ID, submitted.PlanID); err != nil {
			return err
		}
	}
	prompt, err := client.ApprovalPrompt(ctx, submitted.PlanID, "")
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
	decision, err := client.DecideApproval(ctx, prompt, "", action, "local-user", "")
	if err != nil {
		return err
	}
	fmt.Printf("Approval: %s (%s)\n", decision.ID, decision.Decision)
	if !allowed {
		fmt.Println("Plan rejected; no Run was started.")
		return nil
	}

	started, err := client.StartRuns(ctx, gatewayclient.RunStartRequest{
		TaskID: prompt.TaskID, PlanID: prompt.PlanID, PlanRevision: prompt.Revision, PlanHash: prompt.PlanHash,
	})
	if err != nil {
		return err
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
	client, err := gatewayclient.New(mode)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	task, err := client.GetTask(ctx, kernel.TaskID(taskID))
	if err != nil {
		return err
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
	client, err := gatewayclient.New(mode)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	current, err := client.GetTask(ctx, kernel.TaskID(taskID))
	if err != nil {
		return fmt.Errorf("get task before %s: %w", action, err)
	}
	updated, err := client.ControlTask(ctx, kernel.TaskID(taskID), runexecution.TaskAction(action), current.Version, strings.TrimSpace(*reason))
	if err != nil {
		return err
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
	client, err := gatewayclient.New(mode)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	prompt, err := client.ApprovalPrompt(ctx, kernel.PlanID(planID), kernel.StepID(flags.stepID))
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
	if !gatewayclient.ValidApprovalAction(action) {
		return fmt.Errorf("invalid approval action %q", values.action)
	}
	mode, err := paths.ParseMode(values.mode)
	if err != nil {
		return err
	}
	client, err := gatewayclient.New(mode)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	prompt, err := client.ApprovalPrompt(ctx, kernel.PlanID(planID), kernel.StepID(values.stepID))
	if err != nil {
		return err
	}
	if !gatewayclient.PromptAllows(prompt, action, kernel.StepID(values.stepID)) {
		return fmt.Errorf("action %q is not available for the selected approval scope", action)
	}
	decision, err := client.DecideApproval(ctx, prompt, kernel.StepID(values.stepID), action, values.decidedBy, values.reason)
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

func waitForPlanning(ctx context.Context, client *gatewayclient.Client, taskID kernel.TaskID, planID kernel.PlanID) error {
	lastState := ""
	for {
		task, err := client.GetTask(ctx, taskID)
		if err != nil {
			return fmt.Errorf("poll task planning: %w", err)
		}
		if task.State != lastState {
			fmt.Printf("Planning state: %s\n", task.State)
			lastState = task.State
		}
		switch task.State {
		case string(kernel.TaskWaitingApproval):
			if _, err := client.GetPlan(ctx, planID); err != nil {
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

func waitForRuns(ctx context.Context, client *gatewayclient.Client, runIDs []kernel.RunID) ([]corequery.Run, error) {
	results := make([]corequery.Run, len(runIDs))
	states := make(map[kernel.RunID]string, len(runIDs))
	remaining := len(runIDs)
	for remaining > 0 {
		for index, runID := range runIDs {
			if results[index].State == string(kernel.RunCompleted) {
				continue
			}
			run, err := client.GetRun(ctx, runID)
			if err != nil {
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
