package main

import (
	"reflect"
	"testing"
)

func TestCommandFromArgs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		args    []string
		command string
		rest    []string
	}{
		{name: "empty defaults to tui", args: nil, command: "tui", rest: nil},
		{name: "empty slice defaults to tui", args: []string{}, command: "tui", rest: nil},
		{name: "explicit tui", args: []string{"tui"}, command: "tui", rest: []string{}},
		{name: "tui with flags", args: []string{"tui", "--mode", "system"}, command: "tui", rest: []string{"--mode", "system"}},
		{name: "help", args: []string{"help"}, command: "help", rest: []string{}},
		{name: "long help", args: []string{"--help"}, command: "help", rest: []string{}},
		{name: "short help", args: []string{"-h"}, command: "help", rest: []string{}},
		{name: "health", args: []string{"health", "--mode", "user"}, command: "health", rest: []string{"--mode", "user"}},
		{name: "version", args: []string{"version"}, command: "version", rest: []string{}},
		{name: "run", args: []string{"run", "do work"}, command: "run", rest: []string{"do work"}},
		{name: "unknown kept", args: []string{"nope"}, command: "nope", rest: []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			command, rest := commandFromArgs(tt.args)
			if command != tt.command {
				t.Fatalf("command = %q, want %q", command, tt.command)
			}
			if !reflect.DeepEqual(rest, tt.rest) {
				t.Fatalf("rest = %#v, want %#v", rest, tt.rest)
			}
		})
	}
}
