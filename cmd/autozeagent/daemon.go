package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"autozeagent.local/autozeagent/internal/daemonctl"
	"autozeagent.local/autozeagent/internal/platform/paths"
)

const daemonControlTimeout = 30 * time.Second

func runDaemon(args []string) error {
	if len(args) < 1 {
		return errors.New("use autozeagent daemon start|stop|status [--mode user|system]")
	}
	action := args[0]
	switch action {
	case "start", "stop", "status":
	default:
		return errors.New("use autozeagent daemon start|stop|status [--mode user|system]")
	}
	flags := flag.NewFlagSet("daemon "+action, flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	modeValue := flags.String("mode", string(paths.ModeUser), "runtime mode: user or system")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("use autozeagent daemon %s [--mode user|system]", action)
	}
	mode, err := paths.ParseMode(*modeValue)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), daemonControlTimeout)
	defer cancel()
	switch action {
	case "start":
		if err := daemonctl.Start(ctx, mode); err != nil {
			return err
		}
		fmt.Println("daemon started")
		return nil
	case "stop":
		if err := daemonctl.Stop(ctx, mode); err != nil {
			return err
		}
		fmt.Println("daemon stopped")
		return nil
	case "status":
		status, err := daemonctl.Inspect(ctx, mode)
		if err != nil {
			return err
		}
		encoded, err := json.MarshalIndent(status, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(encoded))
		return nil
	default:
		return errors.New("use autozeagent daemon start|stop|status [--mode user|system]")
	}
}

// ensureDaemon starts autozeagentd if needed so gateway-backed commands can proceed.
func ensureDaemon(mode paths.Mode) error {
	ctx, cancel := context.WithTimeout(context.Background(), daemonControlTimeout)
	defer cancel()
	return daemonctl.Ensure(ctx, mode)
}
