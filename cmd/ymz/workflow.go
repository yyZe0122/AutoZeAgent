package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/yyZe0122/yunmengze-agent/internal/gatewayclient"
	"github.com/yyZe0122/yunmengze-agent/internal/kernel"
	"github.com/yyZe0122/yunmengze-agent/internal/platform/paths"
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
	execMode := flags.String("execution-mode", string(kernel.ExecutionModeAgent), "execution mode: agent (build/write) or plan (read-only chat)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 || strings.TrimSpace(flags.Arg(0)) == "" {
		return errors.New("use ymz run [--mode user|system] [--execution-mode agent|plan] \"task objective\"")
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
	em := kernel.NormalizeExecutionMode(*execMode)
	if !em.Valid() {
		return fmt.Errorf("execution-mode must be agent or plan")
	}
	workspace, _ := os.Getwd()
	submitted, err := client.SubmitTask(ctx, gatewayclient.TaskSubmissionRequest{
		Title: gatewayclient.TaskTitle(objective), Objective: objective,
		ExecutionMode: string(em), Workspace: workspace,
	})
	if err != nil {
		return err
	}
	if submitted.Task.ID == "" {
		return errors.New("gateway returned an empty task ID")
	}
	fmt.Printf("Task: %s (%s) mode=%s\n", submitted.Task.ID, submitted.Task.State, em)
	task, err := waitForTaskTerminal(ctx, client, submitted.Task.ID)
	if err != nil {
		return err
	}
	fmt.Printf("Done: %s (%s)\n", task.ID, task.State)
	return nil
}

func waitForTaskTerminal(ctx context.Context, client *gatewayclient.Client, taskID gatewayclient.TaskID) (gatewayclient.Task, error) {
	for {
		if err := ctx.Err(); err != nil {
			return gatewayclient.Task{}, err
		}
		task, err := client.GetTask(ctx, taskID)
		if err != nil {
			return gatewayclient.Task{}, err
		}
		switch task.State {
		case gatewayclient.TaskStateCompleted, gatewayclient.TaskStateFailed, gatewayclient.TaskStateCancelled:
			return task, nil
		}
		select {
		case <-ctx.Done():
			return gatewayclient.Task{}, ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}

func runTaskStatus(args []string) error {
	if len(args) == 0 {
		return errors.New("use ymz task status <task-id> [--mode user|system]")
	}
	taskID := strings.TrimSpace(args[0])
	flags := flag.NewFlagSet("task status", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	modeValue := flags.String("mode", string(paths.ModeUser), "runtime mode: user or system")
	if err := flags.Parse(args[1:]); err != nil {
		return err
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
	task, err := client.GetTask(ctx, gatewayclient.TaskID(taskID))
	if err != nil {
		return err
	}
	return writeJSON(task)
}

func runTaskAction(action string, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("use ymz task %s <task-id> [--reason <text>] [--mode user|system]", action)
	}
	taskID := strings.TrimSpace(args[0])
	flags := flag.NewFlagSet("task "+action, flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	modeValue := flags.String("mode", string(paths.ModeUser), "runtime mode: user or system")
	reason := flags.String("reason", "", "reason")
	if err := flags.Parse(args[1:]); err != nil {
		return err
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
	task, err := client.GetTask(ctx, gatewayclient.TaskID(taskID))
	if err != nil {
		return err
	}
	parsed, ok := gatewayclient.ParseTaskAction(action)
	if !ok {
		return fmt.Errorf("unsupported task action %q", action)
	}
	updated, err := client.ControlTask(ctx, task.ID, parsed, task.Version, *reason)
	if err != nil {
		return err
	}
	return writeJSON(updated)
}

func runApprovalShow(args []string) error {
	return errors.New("interactive plan approval was removed; use Tab plan for read-only chat or ymz run --execution-mode plan")
}

func runApprovalDecide(args []string) error {
	return errors.New("interactive plan approval was removed; use Tab plan for read-only chat or ymz run --execution-mode plan")
}
