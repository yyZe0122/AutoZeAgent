package providerconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadPrefersProjectLocalConfig(t *testing.T) {
	root := t.TempDir()
	userDir := filepath.Join(root, "user")
	projectDir := filepath.Join(root, "project")
	if err := os.MkdirAll(userDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeConfig(t, filepath.Join(userDir, Filename), "user-provider/user-model", "https://user.example")
	writeConfig(t, filepath.Join(projectDir, LocalFilename), "deepseek/deepseek-v4-flash", "https://api.deepseek.com")
	resolved, err := Load(userDir, projectDir)
	if err != nil {
		t.Fatal(err)
	}
	if resolved == nil || resolved.ProviderID != "deepseek" || resolved.ModelID != "deepseek-v4-flash" {
		t.Fatalf("resolved = %+v", resolved)
	}
}

func TestLoadResolvesFileAPIKey(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "key.txt"), []byte("secret-value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	config := `{
  "model": "deepseek/deepseek-v4-flash",
  "provider": {
    "deepseek": {
      "type": "openai-compatible",
      "options": {
        "baseURL": "https://api.deepseek.com",
        "apiKey": "{file:key.txt}",
        "responseFormat": "json_object"
      },
      "models": {"deepseek-v4-flash": {}}
    }
  }
}`
	if err := os.WriteFile(filepath.Join(root, LocalFilename), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	resolved, err := Load(root, root)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.APIKey != "secret-value" || resolved.ResponseFormat != "json_object" {
		t.Fatalf("resolved = %+v", resolved)
	}
}

func TestLoadRejectsUnknownModel(t *testing.T) {
	root := t.TempDir()
	writeConfig(t, filepath.Join(root, LocalFilename), "deepseek/missing", "https://api.deepseek.com")
	if _, err := Load(root, root); err == nil {
		t.Fatal("expected model validation error")
	}
}

func writeConfig(t *testing.T, path, model, baseURL string) {
	t.Helper()
	providerID, _, _ := cutModel(model)
	content := `{
  "model": %q,
  "provider": {
    %q: {
      "options": {"baseURL": %q},
      "models": {"configured-model": {}}
    }
  }
}`
	if model == "deepseek/missing" {
		content = sprintf(content, model, providerID, baseURL)
	} else {
		_, modelID, _ := cutModel(model)
		content = `{
  "model": %q,
  "provider": {
    %q: {
      "options": {"baseURL": %q},
      "models": {%q: {}}
    }
  }
}`
		content = sprintf(content, model, providerID, baseURL, modelID)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func cutModel(value string) (string, string, bool) {
	for i := range value {
		if value[i] == '/' {
			return value[:i], value[i+1:], true
		}
	}
	return value, "", false
}

func sprintf(format string, values ...any) string {
	return fmt.Sprintf(format, values...)
}

func TestResolveProtocolAliases(t *testing.T) {
	tests := map[string]string{
		"openai-compatible": ProtocolOpenAIChat,
		"openai-compat":     ProtocolOpenAIChat,
		"ollama":            ProtocolOpenAIChat,
		"openai":            ProtocolOpenAIResponses,
		"responses":         ProtocolOpenAIResponses,
		"anthropic":         ProtocolAnthropicMessages,
		"claude":            ProtocolAnthropicMessages,
		"gemini":            ProtocolGeminiGenerate,
		"google":            ProtocolGeminiGenerate,
	}
	for alias, expected := range tests {
		t.Run(alias, func(t *testing.T) {
			resolved, err := resolveProtocol("", alias)
			if err != nil {
				t.Fatal(err)
			}
			if resolved != expected {
				t.Fatalf("resolved = %q, want %q", resolved, expected)
			}
		})
	}
}

func TestResolveProtocolRejectsConflictingFields(t *testing.T) {
	if _, err := resolveProtocol(ProtocolAnthropicMessages, "openai-compatible"); err == nil {
		t.Fatal("expected conflict error")
	}
}

func TestLoadResolvesHeaderPlaceholders(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AUTOZEAGENT_TEST_HEADER", "header-secret")
	config := `{
  "model": "custom/model",
  "provider": {
    "custom": {
      "type": "anthropic",
      "options": {
        "baseURL": "https://provider.example",
        "headers": {"X-Custom-Token": "{env:AUTOZEAGENT_TEST_HEADER}"}
      },
      "models": {"model": {}}
    }
  }
}`
	if err := os.WriteFile(filepath.Join(root, LocalFilename), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	resolved, err := Load(root, root)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Protocol != ProtocolAnthropicMessages || resolved.Headers["X-Custom-Token"] != "header-secret" {
		t.Fatalf("resolved = %+v", resolved)
	}
}
