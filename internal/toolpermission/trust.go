package toolpermission

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// TrustFile is the on-disk permanent permission table (ConfigDir, ADR-046).
type TrustFile struct {
	Entries []TrustEntry `json:"entries"`
}

// TrustEntry is one permanent allow pattern.
type TrustEntry struct {
	Capability  string   `json:"capability"`
	PathPrefix  string   `json:"path_prefix,omitempty"`
	Command     string   `json:"command,omitempty"`
	ArgsPrefix  []string `json:"args_prefix,omitempty"`
	CreatedAt   string   `json:"created_at,omitempty"`
	SessionHint string   `json:"session_hint,omitempty"`
}

// TrustStore is an in-memory view of permanent trusts loaded at daemon start.
type TrustStore struct {
	mu      sync.RWMutex
	entries []TrustEntry
	path    string
}

// LoadTrustFile reads permissions-trust.json (missing file → empty).
func LoadTrustFile(path string) (*TrustStore, error) {
	path = strings.TrimSpace(path)
	st := &TrustStore{path: path}
	if path == "" {
		return st, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return st, nil
		}
		return nil, fmt.Errorf("read trust file: %w", err)
	}
	var file TrustFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, fmt.Errorf("decode trust file: %w", err)
	}
	st.entries = append([]TrustEntry(nil), file.Entries...)
	return st, nil
}

// Path returns the configured trust file path.
func (s *TrustStore) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

// Match reports whether a tool call matches a permanent trust entry.
func (s *TrustStore) Match(capability, path, command string, args []string) bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	capName := strings.TrimSpace(capability)
	path = strings.TrimSpace(path)
	command = strings.TrimSpace(command)
	for _, e := range s.entries {
		if strings.TrimSpace(e.Capability) != capName {
			continue
		}
		if pref := strings.TrimSpace(e.PathPrefix); pref != "" {
			if path != pref && !strings.HasPrefix(path, strings.TrimRight(pref, "/")+"/") {
				continue
			}
		}
		if cmd := strings.TrimSpace(e.Command); cmd != "" {
			if command != cmd {
				continue
			}
			if !argsHasPrefix(args, e.ArgsPrefix) {
				continue
			}
		}
		return true
	}
	return false
}

func argsHasPrefix(args, prefix []string) bool {
	if len(prefix) == 0 {
		return true
	}
	if len(args) < len(prefix) {
		return false
	}
	for i := range prefix {
		if args[i] != prefix[i] {
			return false
		}
	}
	return true
}

// AppendTrustEntry adds an entry and rewrites the trust file (atomic replace).
func AppendTrustEntry(path string, entry TrustEntry) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("trust path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	var file TrustFile
	if raw, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(raw, &file)
	}
	file.Entries = append(file.Entries, entry)
	raw, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), ".permissions-trust-*.tmp")
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
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	cleanup = false
	return nil
}

// DefaultTrustPath returns ConfigDir/permissions-trust.json.
func DefaultTrustPath(configDir string) string {
	configDir = strings.TrimSpace(configDir)
	if configDir == "" {
		return ""
	}
	return filepath.Join(configDir, "permissions-trust.json")
}
