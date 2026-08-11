package main

import (
	"testing"
	"time"

	"github.com/yyZe0122/yunmengze-agent/pkg/schedulerapi"
)

func TestParseJobCreateArgs(t *testing.T) {
	values, err := parseJobCreateArgs([]string{
		"--session", "session-1", "--name", "heartbeat", "--every", "15m",
		"--timeout", "2m", "--retries", "2", "--backoff", "5s",
		"--misfire", schedulerapi.MisfireSkip, "inspect progress",
	})
	if err != nil {
		t.Fatal(err)
	}
	if values.sessionID != "session-1" || values.every != 15*time.Minute || values.timeout != 2*time.Minute || values.objective != "inspect progress" {
		t.Fatalf("values = %+v", values)
	}
}

func TestParseJobActionArgs(t *testing.T) {
	jobID, _, reason, err := parseJobActionArgs("pause", []string{"job-1", "--reason", "maintenance"})
	if err != nil {
		t.Fatal(err)
	}
	if jobID != "job-1" || reason != "maintenance" {
		t.Fatalf("jobID=%q reason=%q", jobID, reason)
	}
}
