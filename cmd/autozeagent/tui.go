package main

import (
	"flag"
	"fmt"
	"os"

	"autozeagent.local/autozeagent/internal/platform/paths"
	"autozeagent.local/autozeagent/internal/tui"
)

func runTUI(args []string) error {
	flags := flag.NewFlagSet("tui", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	modeValue := flags.String("mode", string(paths.ModeUser), "runtime mode: user or system")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("use autozeagent tui [--mode user|system]")
	}
	mode, err := paths.ParseMode(*modeValue)
	if err != nil {
		return err
	}
	if err := ensureDaemon(mode); err != nil {
		return err
	}
	return tui.Run(tui.Config{Mode: mode})
}
