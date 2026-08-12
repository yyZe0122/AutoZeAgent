package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/yyZe0122/yunmengze-agent/internal/daemonctl"
	"github.com/yyZe0122/yunmengze-agent/internal/platform/paths"
)

const daemonControlTimeout = 30 * time.Second

func runDaemon(args []string) error {
	if len(args) < 1 {
		return errors.New("use ymz start|stop|restart|status (or ymz daemon start|stop|restart|status) [--mode user|system]")
	}
	return runDaemonAction(args[0], args[1:])
}

// runDaemonAction runs start|stop|status|restart with optional --mode flags.
func runDaemonAction(action string, args []string) error {
	switch action {
	case "start", "stop", "status", "restart":
	default:
		return errors.New("use ymz start|stop|restart|status (or ymz daemon start|stop|restart|status) [--mode user|system]")
	}
	flags := flag.NewFlagSet("daemon "+action, flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	modeValue := flags.String("mode", string(paths.ModeUser), "runtime mode: user or system")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("use ymz %s [--mode user|system]", action)
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
	case "restart":
		if err := daemonctl.Stop(ctx, mode); err != nil {
			return err
		}
		// Fresh context so stop wait does not consume the whole budget for start.
		startCtx, startCancel := context.WithTimeout(context.Background(), daemonControlTimeout)
		defer startCancel()
		if err := daemonctl.Start(startCtx, mode); err != nil {
			return err
		}
		fmt.Println("daemon restarted")
		return nil
	default:
		return errors.New("use ymz start|stop|restart|status [--mode user|system]")
	}
}

// ensureDaemon starts ymzd if needed so gateway-backed commands can proceed.
func ensureDaemon(mode paths.Mode) error {
	ctx, cancel := context.WithTimeout(context.Background(), daemonControlTimeout)
	defer cancel()
	return daemonctl.Ensure(ctx, mode)
}
