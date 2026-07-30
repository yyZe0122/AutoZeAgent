package providerconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadResolvesSelectedModelOptions(t *testing.T) {
	root := t.TempDir()
	config := `{
  "model": "deepseek/deepseek-v4-flash",
  "provider": {
    "deepseek": {
      "type": "openai-compatible",
      "options": {
        "baseURL": "https://api.deepseek.com"
      },
      "models": {
        "deepseek-v4-flash": {
          "name": "DeepSeek V4 Flash",
          "temperature": 0.25,
          "maxTokens": 2048,
          "reasoningEffort": "high"
        }
      }
    }
  }
}`
	if err := os.WriteFile(filepath.Join(root, LocalFilename), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	resolved, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Temperature == nil || *resolved.Temperature != 0.25 {
		t.Fatalf("temperature = %v", resolved.Temperature)
	}
	if resolved.MaxTokens != 2048 || resolved.ReasoningEffort != "high" {
		t.Fatalf("resolved model options = %+v", resolved)
	}
	if resolved.ResponseFormat != "" {
		t.Fatalf("response format = %q, want automatic", resolved.ResponseFormat)
	}
}

func TestLoadRejectsInvalidSelectedModelOptions(t *testing.T) {
	tests := []struct {
		name       string
		provider   string
		modelBlock string
		want       string
	}{
		{name: "temperature", provider: "openai-compatible", modelBlock: `"temperature": 2.1`, want: "temperature"},
		{name: "max tokens", provider: "openai-compatible", modelBlock: `"maxTokens": -1`, want: "maxTokens"},
		{name: "unsupported reasoning", provider: "anthropic", modelBlock: `"reasoningEffort": "high"`, want: "reasoningEffort"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			config := `{
  "model": "test/model",
  "provider": {
    "test": {
      "type": "` + test.provider + `",
      "options": {"baseURL": "https://provider.example"},
      "models": {"model": {` + test.modelBlock + `}}
    }
  }
}`
			if err := os.WriteFile(filepath.Join(root, LocalFilename), []byte(config), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := Load(root)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}
