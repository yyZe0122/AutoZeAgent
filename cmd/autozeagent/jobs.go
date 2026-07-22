package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"autozeagent.local/autozeagent/internal/platform/paths"
	"autozeagent.local/autozeagent/pkg/schedulerapi"
)

type jobCreateValues struct {
	mode           string
	sessionID      string
	name           string
	title          string
	every          time.Duration
	start          string
	timeout        time.Duration
	maxRetries     int
	backoff        time.Duration
	misfirePolicy  string
	idempotencyKey string
	objective      string
}

func runJobCreate(args []string) error {
	values, err := parseJobCreateArgs(args)
	if err != nil {
		return err
	}
	mode, err := paths.ParseMode(values.mode)
	if err != nil {
		return err
	}
	if values.idempotencyKey == "" {
		values.idempotencyKey, err = randomWorkflowID("job-")
		if err != nil {
			return err
		}
	}
	if values.title == "" {
		values.title = taskTitle(values.objective)
	}
	client, err := newWorkflowClient(mode)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	var job schedulerapi.Job
	if err := client.DoJSON(ctx, http.MethodPost, "/v1/jobs", schedulerapi.CreateRequest{
		Name: values.name, SessionID: values.sessionID, TaskTitle: values.title, TaskObjective: values.objective,
		IntervalSeconds: int64(values.every / time.Second), NextRunAt: values.start,
		TimeoutSeconds: int64(values.timeout / time.Second), MaxRetries: values.maxRetries,
		BackoffSeconds: int64(values.backoff / time.Second), MisfirePolicy: values.misfirePolicy,
		IdempotencyKey: values.idempotencyKey,
	}, &job); err != nil {
		return fmt.Errorf("create job: %w", err)
	}
	return writeJSON(job)
}

func parseJobCreateArgs(args []string) (jobCreateValues, error) {
	values := jobCreateValues{}
	flags := flag.NewFlagSet("job create", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	flags.StringVar(&values.mode, "mode", string(paths.ModeUser), "runtime mode: user or system")
	flags.StringVar(&values.sessionID, "session", "", "existing session ID")
	flags.StringVar(&values.name, "name", "", "job name")
	flags.StringVar(&values.title, "title", "", "task title")
	flags.DurationVar(&values.every, "every", 0, "heartbeat interval, for example 15m or 1h")
	flags.StringVar(&values.start, "start", "", "first run time in RFC3339; default is now")
	flags.DurationVar(&values.timeout, "timeout", 30*time.Minute, "scheduled task timeout")
	flags.IntVar(&values.maxRetries, "retries", 3, "maximum retry count")
	flags.DurationVar(&values.backoff, "backoff", 30*time.Second, "base retry backoff")
	flags.StringVar(&values.misfirePolicy, "misfire", schedulerapi.MisfireRunOnce, "run_once, skip, or catch_up")
	flags.StringVar(&values.idempotencyKey, "idempotency-key", "", "stable key used to avoid duplicate jobs")
	if err := flags.Parse(args); err != nil {
		return jobCreateValues{}, err
	}
	if flags.NArg() != 1 {
		return jobCreateValues{}, fmt.Errorf("use autozeagent job create --session <id> --name <name> --every <duration> [options] \"objective\"")
	}
	values.sessionID = strings.TrimSpace(values.sessionID)
	values.name = strings.TrimSpace(values.name)
	values.title = strings.TrimSpace(values.title)
	values.objective = strings.TrimSpace(flags.Arg(0))
	values.start = strings.TrimSpace(values.start)
	values.idempotencyKey = strings.TrimSpace(values.idempotencyKey)
	values.misfirePolicy = strings.ToLower(strings.TrimSpace(values.misfirePolicy))
	if values.sessionID == "" || values.name == "" || values.objective == "" || values.every < time.Second || values.timeout < time.Second || values.maxRetries < 0 || values.backoff < 0 {
		return jobCreateValues{}, fmt.Errorf("session, name, objective, positive --every/--timeout, and non-negative retry settings are required")
	}
	if values.start != "" {
		parsed, err := time.Parse(time.RFC3339Nano, values.start)
		if err != nil {
			return jobCreateValues{}, fmt.Errorf("parse --start: %w", err)
		}
		values.start = parsed.UTC().Format(time.RFC3339Nano)
	}
	switch values.misfirePolicy {
	case schedulerapi.MisfireRunOnce, schedulerapi.MisfireSkip, schedulerapi.MisfireCatchUp:
	default:
		return jobCreateValues{}, fmt.Errorf("invalid --misfire %q", values.misfirePolicy)
	}
	return values, nil
}

func runJobList(args []string) error {
	flags := flag.NewFlagSet("job list", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	modeValue := flags.String("mode", string(paths.ModeUser), "runtime mode: user or system")
	includeArchived := flags.Bool("all", false, "include canceled jobs")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("use autozeagent job list [--all] [--mode user|system]")
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
	path := "/v1/jobs"
	if *includeArchived {
		path += "?include_archived=true"
	}
	var response struct {
		Jobs []schedulerapi.Job `json:"jobs"`
	}
	if err := client.DoJSON(ctx, http.MethodGet, path, nil, &response); err != nil {
		return fmt.Errorf("list jobs: %w", err)
	}
	return writeJSON(response)
}

func runJobStatus(args []string) error {
	jobID, mode, err := parseJobTarget("job status", args, false)
	if err != nil {
		return err
	}
	client, err := newWorkflowClient(mode)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	var job schedulerapi.Job
	if err := client.DoJSON(ctx, http.MethodGet, "/v1/jobs/"+url.PathEscape(jobID), nil, &job); err != nil {
		return fmt.Errorf("get job: %w", err)
	}
	return writeJSON(job)
}

func runJobAction(action string, args []string) error {
	jobID, mode, reason, err := parseJobActionArgs(action, args)
	if err != nil {
		return err
	}
	client, err := newWorkflowClient(mode)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	var job schedulerapi.Job
	if err := client.DoJSON(ctx, http.MethodPost, "/v1/jobs/"+url.PathEscape(jobID)+"/actions", map[string]any{
		"action": action, "reviewer": "local-user", "reason": reason,
	}, &job); err != nil {
		return fmt.Errorf("%s job: %w", action, err)
	}
	return writeJSON(job)
}

func parseJobTarget(command string, args []string, requireReason bool) (string, paths.Mode, error) {
	jobID, mode, _, err := parseJobTargetValues(command, args, requireReason)
	return jobID, mode, err
}

func parseJobActionArgs(action string, args []string) (string, paths.Mode, string, error) {
	return parseJobTargetValues("job "+action, args, true)
}

func parseJobTargetValues(command string, args []string, allowReason bool) (string, paths.Mode, string, error) {
	if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
		return "", "", "", fmt.Errorf("use autozeagent %s <job-id> [options]", command)
	}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	modeValue := flags.String("mode", string(paths.ModeUser), "runtime mode: user or system")
	var reason *string
	if allowReason {
		reason = flags.String("reason", "operator request", "reason recorded with the job state change")
	}
	if err := flags.Parse(args[1:]); err != nil {
		return "", "", "", err
	}
	if flags.NArg() != 0 {
		return "", "", "", fmt.Errorf("use autozeagent %s <job-id> [options]", command)
	}
	mode, err := paths.ParseMode(*modeValue)
	if err != nil {
		return "", "", "", err
	}
	value := ""
	if reason != nil {
		value = strings.TrimSpace(*reason)
		if value == "" {
			return "", "", "", fmt.Errorf("job action reason is required")
		}
	}
	return strings.TrimSpace(args[0]), mode, value, nil
}
