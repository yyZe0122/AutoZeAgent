package agent

import (
	"testing"

	"autozeagent.local/autozeagent/pkg/providerapi"
)

func TestToolStepSignatureStable(t *testing.T) {
	a := []providerapi.ToolCall{{ID: "1", Name: "fs_read", Arguments: `{"path":"a"}`}}
	b := []providerapi.ToolCall{{ID: "2", Name: "fs_read", Arguments: `{"path":"a"}`}}
	if toolStepSignature(a) != toolStepSignature(b) {
		t.Fatal("signature should ignore call id")
	}
	c := []providerapi.ToolCall{{ID: "1", Name: "fs_read", Arguments: `{"path":"b"}`}}
	if toolStepSignature(a) == toolStepSignature(c) {
		t.Fatal("signature should change with args")
	}
}

func TestHasRepeatedToolSteps(t *testing.T) {
	sig := "abc"
	var sigs []string
	for i := 0; i < 10; i++ {
		sigs = append(sigs, sig)
	}
	if !hasRepeatedToolSteps(sigs, 10, 5) {
		t.Fatal("expected loop")
	}
	if hasRepeatedToolSteps(sigs[:4], 10, 5) {
		t.Fatal("window not full")
	}
	mixed := []string{"a", "b", "a", "b", "a", "b", "a", "b", "a", "b"}
	if hasRepeatedToolSteps(mixed, 10, 5) {
		t.Fatal("alternating should not exceed maxRepeats for one sig")
	}
}
