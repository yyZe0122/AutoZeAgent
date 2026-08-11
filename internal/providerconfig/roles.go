package providerconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func LoadModelRoles(configDir string) (main string, roles map[string]string, err error) {
	path, err := findConfigPath(configDir)
	if err != nil {
		return "", nil, err
	}
	if path == "" {
		return "", nil, nil
	}
	config, err := decodeConfigFile(path)
	if err != nil {
		return "", nil, err
	}
	if err := validateModelsMap(config); err != nil {
		return "", nil, err
	}
	main = strings.TrimSpace(config.Model)
	if len(config.Models) == 0 {
		return main, nil, nil
	}
	roles = make(map[string]string, len(config.Models))
	for role, ref := range config.Models {
		role = strings.TrimSpace(role)
		ref = strings.TrimSpace(ref)
		if ref == "" {
			continue
		}
		roles[role] = ref
	}
	if len(roles) == 0 {
		return main, nil, nil
	}
	return main, roles, nil
}

// ResolveModel loads configuration and resolves provider/model for the given ref.
// ref must be provider/model. The selected top-level model field is overridden.
func ResolveModel(configDir, ref string) (*Resolved, error) {
	if err := LoadEnvFromConfigDir(configDir); err != nil {
		return nil, err
	}
	ref = strings.TrimSpace(ref)
	providerID, modelID, ok := strings.Cut(ref, "/")
	providerID, modelID = strings.TrimSpace(providerID), strings.TrimSpace(modelID)
	if !ok || providerID == "" || modelID == "" {
		return nil, errors.New("model must use provider/model format")
	}
	path, err := findConfigPath(configDir)
	if err != nil {
		return nil, err
	}
	if path == "" {
		return nil, errors.New("provider config not found")
	}
	resolved, err := loadFileWithModel(path, providerID, modelID)
	if err != nil {
		return nil, err
	}
	return &resolved, nil
}

// WriteSelectedModel updates the top-level model field in the first resolvable
// config file under configDir. Other fields and secrets are preserved.
func WriteSelectedModel(configDir, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	providerID, modelID, ok := strings.Cut(ref, "/")
	providerID, modelID = strings.TrimSpace(providerID), strings.TrimSpace(modelID)
	if !ok || providerID == "" || modelID == "" {
		return "", errors.New("model must use provider/model format")
	}
	selected := providerID + "/" + modelID
	path, err := findConfigPath(configDir)
	if err != nil {
		return "", err
	}
	if path == "" {
		return "", errors.New("provider config not found")
	}
	if err := validateModelInFile(path, providerID, modelID); err != nil {
		return "", err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read provider config %s: %w", path, err)
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(raw, &document); err != nil {
		return "", fmt.Errorf("decode provider config %s: %w", path, err)
	}
	encoded, err := json.Marshal(selected)
	if err != nil {
		return "", err
	}
	document["model"] = encoded
	updated, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return "", err
	}
	updated = append(updated, '\n')
	info, err := os.Stat(path)
	mode := os.FileMode(0o600)
	if err == nil {
		mode = info.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".autozeagent-model-*.tmp")
	if err != nil {
		return "", fmt.Errorf("create temp config: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(updated); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("write temp config: %w", err)
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("chmod temp config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("close temp config: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return "", fmt.Errorf("replace provider config %s: %w", path, err)
	}
	cleanup = false
	return path, nil
}
