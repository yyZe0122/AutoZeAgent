package main

import (
	"errors"
	"fmt"
	"os"

	"autozeagent.local/autozeagent/internal/version"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "autozeagent:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	command, rest := commandFromArgs(args)
	switch command {
	case "tui":
		return runTUI(rest)
	case "version":
		if len(rest) != 0 {
			return errors.New("version does not accept arguments")
		}
		fmt.Printf("autozeagent %s commit=%s date=%s\n", version.Version, version.Commit, version.Date)
		return nil
	case "paths":
		return runPaths(rest)
	case "config":
		if len(rest) < 1 || rest[0] != "validate" {
			return errors.New("use autozeagent config validate [--mode user|system]")
		}
		return runConfigValidate(rest[1:])
	case "health":
		return runHealth(rest)
	case "logs":
		return runLogs(rest)
	case "daemon":
		return runDaemon(rest)
	case "run":
		return runWorkflow(rest)
	case "task":
		if len(rest) < 1 {
			return errors.New("use autozeagent task status|pause|resume|cancel ...")
		}
		switch rest[0] {
		case "status":
			return runTaskStatus(rest[1:])
		case "pause", "resume", "cancel":
			return runTaskAction(rest[0], rest[1:])
		default:
			return errors.New("use autozeagent task status|pause|resume|cancel ...")
		}
	case "job":
		if len(rest) < 1 {
			return errors.New("use autozeagent job create|list|status|pause|resume|cancel ...")
		}
		switch rest[0] {
		case "create":
			return runJobCreate(rest[1:])
		case "list":
			return runJobList(rest[1:])
		case "status":
			return runJobStatus(rest[1:])
		case "pause", "resume", "cancel":
			return runJobAction(rest[0], rest[1:])
		default:
			return errors.New("use autozeagent job create|list|status|pause|resume|cancel ...")
		}
	case "approval":
		if len(rest) < 1 {
			return errors.New("use autozeagent approval show|decide ...")
		}
		switch rest[0] {
		case "show":
			return runApprovalShow(rest[1:])
		case "decide":
			return runApprovalDecide(rest[1:])
		default:
			return errors.New("use autozeagent approval show|decide ...")
		}
	case "db":
		if len(rest) < 1 || rest[0] != "check" {
			return errors.New("use autozeagent db check [--mode user|system]")
		}
		return runDBCheck(rest[1:])
	case "help":
		printUsage()
		return nil
	default:
		return errors.New("unknown command; use autozeagent help")
	}
}

// commandFromArgs maps CLI argv to a command name and remaining args.
// Empty argv defaults to the interactive TUI (same as opencode/crush).
func commandFromArgs(args []string) (command string, rest []string) {
	if len(args) == 0 {
		return "tui", nil
	}
	switch args[0] {
	case "help", "--help", "-h":
		return "help", args[1:]
	default:
		return args[0], args[1:]
	}
}

func printUsage() {
	fmt.Println("AutoZeAgent local client")
	fmt.Println()
	fmt.Println("With no arguments, starts the interactive TUI (also available as aze).")
	fmt.Println("TUI and run auto-start the unique local daemon; use daemon stop to shut it down.")

	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  autozeagent | aze")
	fmt.Println("  autozeagent tui [--mode user|system]")
	fmt.Println("  autozeagent daemon start|stop|status [--mode user|system]")
	fmt.Println("  autozeagent version")
	fmt.Println("  autozeagent paths [user|system]")
	fmt.Println("  autozeagent config validate [--mode user|system]")
	fmt.Println("  autozeagent health [--mode user|system]")
	fmt.Println("  autozeagent logs [--mode user|system] [--tail 200] [--level error] [--component agent] [--run run-id]")
	fmt.Println("  autozeagent run [--mode user|system] \"task objective\"")

	fmt.Println("  autozeagent task status <task-id> [--mode user|system]")
	fmt.Println("  autozeagent task pause|resume|cancel <task-id> [--reason <text>] [--mode user|system]")
	fmt.Println("  autozeagent job create --session <id> --name <name> --every <duration> [options] \"objective\"")
	fmt.Println("  autozeagent job list|status|pause|resume|cancel ...")
	fmt.Println("  autozeagent approval show <plan-id> [--step <step-id>] [--mode user|system]")
	fmt.Println("  autozeagent approval decide <plan-id> --action <action> [--step <step-id>] [--reason <text>] [--mode user|system]")
	fmt.Println("  autozeagent db check [--mode user|system]")
	fmt.Println("  autozeagent help")
}
