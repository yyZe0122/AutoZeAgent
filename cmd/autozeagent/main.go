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
	if len(args) == 0 {
		printUsage()
		return nil
	}
	switch args[0] {
	case "version":
		if len(args) != 1 {
			return errors.New("version does not accept arguments")
		}
		fmt.Printf("autozeagent %s commit=%s date=%s\n", version.Version, version.Commit, version.Date)
		return nil
	case "paths":
		return runPaths(args[1:])
	case "config":
		if len(args) < 2 || args[1] != "validate" {
			return errors.New("use autozeagent config validate [--mode user|system]")
		}
		return runConfigValidate(args[2:])
	case "health":
		return runHealth(args[1:])
	case "logs":
		return runLogs(args[1:])
	case "run":
		return runWorkflow(args[1:])
	case "task":
		if len(args) < 2 {
			return errors.New("use autozeagent task status|pause|resume|cancel ...")
		}
		switch args[1] {
		case "status":
			return runTaskStatus(args[2:])
		case "pause", "resume", "cancel":
			return runTaskAction(args[1], args[2:])
		default:
			return errors.New("use autozeagent task status|pause|resume|cancel ...")
		}
	case "job":
		if len(args) < 2 {
			return errors.New("use autozeagent job create|list|status|pause|resume|cancel ...")
		}
		switch args[1] {
		case "create":
			return runJobCreate(args[2:])
		case "list":
			return runJobList(args[2:])
		case "status":
			return runJobStatus(args[2:])
		case "pause", "resume", "cancel":
			return runJobAction(args[1], args[2:])
		default:
			return errors.New("use autozeagent job create|list|status|pause|resume|cancel ...")
		}
	case "approval":
		if len(args) < 2 {
			return errors.New("use autozeagent approval show|decide ...")
		}
		switch args[1] {
		case "show":
			return runApprovalShow(args[2:])
		case "decide":
			return runApprovalDecide(args[2:])
		default:
			return errors.New("use autozeagent approval show|decide ...")
		}
	case "db":
		if len(args) < 2 || args[1] != "check" {
			return errors.New("use autozeagent db check [--mode user|system]")
		}
		return runDBCheck(args[2:])
	case "help", "--help", "-h":
		printUsage()
		return nil
	default:
		return errors.New("unknown command; use autozeagent help")
	}
}

func printUsage() {
	fmt.Println("AutoZeAgent local client")
	fmt.Println()
	fmt.Println("Usage:")
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
}
