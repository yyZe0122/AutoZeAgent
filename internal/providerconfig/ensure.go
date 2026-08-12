package providerconfig

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func findConfigPath(configDir string) (string, error) {
	configDir = strings.TrimSpace(configDir)
	if configDir == "" {
		return "", errors.New("config directory is required")
	}
	candidates := []string{
		filepath.Join(configDir, LocalFilename),
		filepath.Join(configDir, Filename),
	}
	for _, candidate := range candidates {
		info, err := os.Stat(candidate)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("inspect provider config %s: %w", candidate, err)
		}
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("provider config is not a regular file: %s", candidate)
		}
		return candidate, nil
	}
	return "", nil
}

// EnsureResult describes how provider config was made available under configDir.
type EnsureResult struct {
	Path     string // active config path under configDir (may be empty only on error)
	Created  bool   // wrote the default template
	Migrated bool   // copied from a project/legacy path
	Source   string // migration source path when Migrated
}

// EnsureConfig prepares configDir for provider loading:
//  1. MkdirAll configDir
//  2. If ConfigDir already has a config file, use it
//  3. Else copy the first existing file from migrateFromDirs (each dir checked for
//     agent.local.json then agent.json) into ConfigDir as LocalFilename
//  4. Else write a default template (env-based keys, no secrets) as Filename
func EnsureConfig(configDir string, migrateFromDirs ...string) (EnsureResult, error) {
	configDir = strings.TrimSpace(configDir)
	if configDir == "" {
		return EnsureResult{}, errors.New("config directory is required")
	}
	if err := os.MkdirAll(configDir, 0o750); err != nil {
		return EnsureResult{}, fmt.Errorf("create config directory %s: %w", configDir, err)
	}
	// Optional ConfigDir/env (installers may seed it). Never fails the ensure path on load errors beyond I/O.
	if err := LoadEnvFromConfigDir(configDir); err != nil {
		return EnsureResult{}, err
	}
	if _, _, err := EnsureEnvFile(configDir); err != nil {
		return EnsureResult{}, err
	}
	existing, err := findConfigPath(configDir)
	if err != nil {
		return EnsureResult{}, err
	}
	if existing != "" {
		return EnsureResult{Path: existing}, nil
	}
	for _, dir := range migrateFromDirs {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		for _, name := range []string{LocalFilename, Filename} {
			src := filepath.Join(dir, name)
			info, statErr := os.Stat(src)
			if errors.Is(statErr, os.ErrNotExist) {
				continue
			}
			if statErr != nil {
				return EnsureResult{}, fmt.Errorf("inspect migrate source %s: %w", src, statErr)
			}
			if !info.Mode().IsRegular() {
				continue
			}
			dst := filepath.Join(configDir, LocalFilename)
			if err := copyFile(src, dst, 0o600); err != nil {
				return EnsureResult{}, fmt.Errorf("migrate provider config %s → %s: %w", src, dst, err)
			}
			return EnsureResult{Path: dst, Migrated: true, Source: src}, nil
		}
	}
	dst := filepath.Join(configDir, Filename)
	if err := os.WriteFile(dst, []byte(defaultConfigTemplate), 0o600); err != nil {
		return EnsureResult{}, fmt.Errorf("write default provider config %s: %w", dst, err)
	}
	return EnsureResult{Path: dst, Created: true}, nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	raw, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".yunmengze-config-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, dst); err != nil {
		return err
	}
	cleanup = false
	return nil
}

// defaultConfigTemplate is written when ConfigDir has no config and nothing to migrate.
// Keys use {env:…}; no literal secrets.
const defaultConfigTemplate = `{
  "model": "deepseek1/deepseek-chat",
  "provider": {
    "deepseek1": {
      "type": "openai-compatible",
      "options": {
        "baseURL": "https://api.deepseek.com/v1",
        "apiKey": "{env:DEEPSEEK1_API_KEY}"
      },
      "models": {
        "deepseek-chat": {
          "name": "DeepSeek Chat"
        }
      }
    },
    "deepseek2": {
      "type": "openai-compatible",
      "options": {
        "baseURL": "https://llm.example.com/v1",
        "apiKey": "{env:DEEPSEEK2_API_KEY}"
      },
      "models": {
        "deepseek/deepseek-v4-flash": {
          "name": "Nested wire id (select deepseek2/deepseek/deepseek-v4-flash)"
        },
        "flash": {
          "name": "Short key + id override (select deepseek2/flash)",
          "id": "deepseek/deepseek-v4-flash"
        }
      }
    }
  }
}
`
