package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/yyZe0122/yunmengze-agent/internal/app"
	"github.com/yyZe0122/yunmengze-agent/internal/gatewayclient"
	"github.com/yyZe0122/yunmengze-agent/internal/opencodeimport"
	"github.com/yyZe0122/yunmengze-agent/internal/platform/paths"
	"github.com/yyZe0122/yunmengze-agent/internal/providerconfig"
)

type configCheck struct {
	OK         bool       `json:"ok"`
	Mode       paths.Mode `json:"mode"`
	PathsOK    bool       `json:"paths_ok"`
	ProviderOK bool       `json:"provider_ok"`
	Model      string     `json:"model,omitempty"`
	ProviderID string     `json:"provider_id,omitempty"`
	Source     string     `json:"source,omitempty"`
	// ChatAgentWriteCeiling reports whether agent mode may write (plan is always RO).
	ChatAgentWriteCeiling bool `json:"chat_agent_write_ceiling"`
}

type databaseCheck struct {
	Path   string `json:"path"`
	Result string `json:"result"`
}

type healthCheck struct {
	OK   bool       `json:"ok"`
	Core app.Status `json:"core"`
}

type dbCheck struct {
	OK        bool            `json:"ok"`
	Mode      paths.Mode      `json:"mode"`
	Databases []databaseCheck `json:"databases"`
}

func runPaths(args []string) error {
	mode := paths.ModeUser
	if len(args) > 1 {
		return errors.New("paths accepts at most one mode")
	}
	if len(args) == 1 {
		parsed, err := paths.ParseMode(args[0])
		if err != nil {
			return err
		}
		mode = parsed
	}
	layout, err := paths.Resolve(mode)
	if err != nil {
		return err
	}
	return writeJSON(layout)
}

func runConfigValidate(args []string) error {
	mode, err := parseMode("config validate", args)
	if err != nil {
		return err
	}
	check, err := validateConfig(mode)
	if err != nil {
		return err
	}
	return writeJSON(check)
}

func runConfigImportOpenCode(args []string) error {
	flags := flag.NewFlagSet("config import-opencode", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	modeValue := flags.String("mode", string(paths.ModeUser), "runtime mode: user or system")
	dryRun := flags.Bool("dry-run", false, "print mapped config and warnings without writing")
	output := flags.String("output", "", "write path (default: <config-dir>/agent.local.json)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	src := ""
	if flags.NArg() == 1 {
		src = flags.Arg(0)
	} else if flags.NArg() > 1 {
		return errors.New("use ymz config import-opencode [path] [--mode user|system] [--dry-run] [--output path]")
	}
	if src == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("resolve home for default opencode path: %w", err)
		}
		// Prefer XDG-style then legacy ~/.opencode
		candidates := []string{
			filepath.Join(home, ".config", "opencode", "opencode.json"),
			filepath.Join(home, ".config", "opencode", "opencode.jsonc"),
			filepath.Join(home, ".opencode", "opencode.json"),
			filepath.Join(home, ".opencode", "opencode.jsonc"),
		}
		for _, c := range candidates {
			if st, err := os.Stat(c); err == nil && st.Mode().IsRegular() {
				src = c
				break
			}
		}
		if src == "" {
			return errors.New("opencode config path required (no default found under ~/.config/opencode or ~/.opencode)")
		}
	}
	mode, err := paths.ParseMode(*modeValue)
	if err != nil {
		return err
	}
	layout, err := paths.Resolve(mode)
	if err != nil {
		return err
	}
	res, err := opencodeimport.ConvertFile(src)
	if err != nil {
		return err
	}
	for _, w := range res.Warnings {
		fmt.Fprintln(os.Stderr, "warning:", w)
	}
	if *dryRun {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(res.File); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "dry-run: would write from %s (%d warnings)\n", res.Source, len(res.Warnings))
		return nil
	}
	outPath := strings.TrimSpace(*output)
	if outPath == "" {
		outPath, err = opencodeimport.WriteLocal(layout.ConfigDir, res.File)
		if err != nil {
			return err
		}
	} else {
		data, err := json.MarshalIndent(res.File, "", "  ")
		if err != nil {
			return err
		}
		data = append(data, '\n')
		if err := os.MkdirAll(filepath.Dir(outPath), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(outPath, data, 0o600); err != nil {
			return err
		}
	}
	fmt.Fprintf(os.Stdout, "imported %s → %s (%d warnings)\n", res.Source, outPath, len(res.Warnings))
	fmt.Fprintln(os.Stderr, "run: ymz config validate")
	return nil
}

func runHealth(args []string) error {
	mode, err := parseMode("health", args)
	if err != nil {
		return err
	}
	layout, err := paths.Resolve(mode)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := queryGatewayHealth(ctx, layout.RuntimeDir)
	if err != nil {
		return err
	}
	return writeJSON(result)
}

func queryGatewayHealth(ctx context.Context, runtimeDir string) (healthCheck, error) {
	if ctx == nil {
		return healthCheck{}, errors.New("health check context is required")
	}
	var lastErr error
	for {
		client, err := gatewayclient.NewFromRuntimeDir(runtimeDir)
		if err == nil {
			result, healthErr := client.Health(ctx)
			if healthErr == nil {
				return healthCheck{OK: result.OK, Core: result.Core}, nil
			}
			err = healthErr
		}
		lastErr = err
		select {
		case <-ctx.Done():
			if lastErr == nil {
				lastErr = ctx.Err()
			}
			return healthCheck{}, fmt.Errorf("local gateway health check: %w", lastErr)
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func runDBCheck(args []string) error {
	mode, err := parseMode("db check", args)
	if err != nil {
		return err
	}
	layout, err := paths.Resolve(mode)
	if err != nil {
		return err
	}
	files, err := databaseFiles(layout.DataDir)
	if err != nil {
		return err
	}
	corePath := filepath.Join(layout.DataDir, "core.db")
	if !containsPath(files, corePath) {
		return fmt.Errorf("core database is missing: %s", corePath)
	}
	checks := make([]databaseCheck, 0, len(files))
	for _, file := range files {
		check, err := checkSQLite(context.Background(), file)
		if err != nil {
			return fmt.Errorf("database check %s: %w", file, err)
		}
		checks = append(checks, check)
	}
	return writeJSON(dbCheck{OK: true, Mode: mode, Databases: checks})
}

func parseMode(command string, args []string) (paths.Mode, error) {
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	value := flags.String("mode", string(paths.ModeUser), "runtime mode: user or system")
	if err := flags.Parse(args); err != nil {
		return "", err
	}
	if flags.NArg() != 0 {
		return "", fmt.Errorf("%s does not accept positional arguments", command)
	}
	return paths.ParseMode(*value)
}

func validateConfig(mode paths.Mode) (configCheck, error) {
	layout, err := paths.Resolve(mode)
	if err != nil {
		return configCheck{}, err
	}
	if err := layout.Validate(); err != nil {
		return configCheck{}, err
	}
	check := configCheck{Mode: mode, PathsOK: true}
	chat, err := providerconfig.LoadChat(layout.ConfigDir)
	if err != nil {
		return configCheck{}, fmt.Errorf("chat config: %w", err)
	}
	check.ChatAgentWriteCeiling = chat.AgentWriteCeiling()
	if _, err := providerconfig.LoadMCP(layout.ConfigDir); err != nil {
		return configCheck{}, fmt.Errorf("mcp config: %w", err)
	}
	resolved, err := providerconfig.Load(layout.ConfigDir)
	if err != nil {
		return configCheck{}, fmt.Errorf("provider config: %w", err)
	}
	if resolved == nil {
		return configCheck{}, errors.New("provider config not found under config directory")
	}
	check.ProviderOK = true
	check.Model = resolved.SelectionRef
	if check.Model == "" {
		check.Model = resolved.ProviderID + "/" + resolved.ModelID
	}
	check.ProviderID = resolved.ProviderID
	check.Source = resolved.Source
	check.OK = true
	return check, nil
}

func databaseFiles(root string) ([]string, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("data directory: %w", err)
	}
	if !info.IsDir() {
		return nil, errors.New("data path is not a directory")
	}
	var files []string
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".db") {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			files = append(files, filepath.Clean(path))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

func checkSQLite(ctx context.Context, path string) (databaseCheck, error) {
	if ctx == nil {
		return databaseCheck{}, errors.New("database check context is required")
	}
	path = filepath.Clean(path)
	info, err := os.Stat(path)
	if err != nil {
		return databaseCheck{}, err
	}
	if !info.Mode().IsRegular() {
		return databaseCheck{}, errors.New("database is not a regular file")
	}
	slashPath := filepath.ToSlash(path)
	if filepath.VolumeName(path) != "" {
		slashPath = "/" + slashPath
	}
	uri := &url.URL{Scheme: "file", Path: slashPath}
	query := uri.Query()
	query.Set("mode", "ro")
	uri.RawQuery = query.Encode()
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return databaseCheck{}, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	var result string
	if err := db.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&result); err != nil {
		return databaseCheck{}, err
	}
	if result != "ok" {
		return databaseCheck{}, fmt.Errorf("quick_check returned %q", result)
	}
	return databaseCheck{Path: path, Result: result}, nil
}

func containsPath(files []string, target string) bool {
	target = filepath.Clean(target)
	for _, file := range files {
		if filepath.Clean(file) == target {
			return true
		}
	}
	return false
}

func writeJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
