# Provider protocol configuration

AutoZeAgent selects a wire protocol from each provider's JSON configuration. The provider ID and URL do not imply a protocol, so the same gateway URL can expose different adapters by changing only `type` or `protocol`.

Configuration is loaded **only** from the OS config directory (`paths.Layout.ConfigDir`):

1. `<config-dir>/autozeagent.local.json` (machine-local; preferred)
2. `<config-dir>/autozeagent.json`
3. Optional `<config-dir>/env` — `KEY=value` lines loaded into the process before resolving `{env:…}` (does **not** override variables already set in the environment)

| Mode | Linux | Windows | macOS |
| --- | --- | --- | --- |
| user | `~/.config/autozeagent` (`XDG_CONFIG_HOME`) | `%APPDATA%\AutoZeAgent` | Application Support path from `paths` |
| system | `/etc/autozeagent` | `ProgramData\AutoZeAgent\config` | system path from `paths` |

On first start, if ConfigDir has no file, the daemon may **migrate** once from the process working directory or data dir (legacy project `autozeagent.local.json`), otherwise it writes a default template with `{env:…}` placeholders (no secrets) and may seed an empty `env` template. Installers do the same without overwriting existing files. Project directories are **not** searched for ongoing loads.

## API keys (choose any; nothing is forced)

`options.apiKey` supports three forms:

| Form | Example | Notes |
| --- | --- | --- |
| Environment placeholder | `"{env:DEEPSEEK_API_KEY}"` | **Recommended.** Value from process env and/or ConfigDir `env` file |
| File reference | `"{file:secrets/key.txt}"` | Path relative to ConfigDir, or absolute |
| Literal string | `"sk-..."` | Allowed for local convenience; protect file permissions; never commit |

Examples:

```json
"apiKey": "{env:DEEPSEEK_API_KEY}"
```

```json
"apiKey": "{file:secrets/deepseek.key}"
```

```json
"apiKey": "sk-your-key-here"
```

Optional ConfigDir `env` file:

```bash
# ~/.config/autozeagent/env  (chmod 600)
DEEPSEEK_API_KEY=sk-...
```

See also [`configs/autozeagent.json.example`](../configs/autozeagent.json.example) (includes env / file / literal illustrations).

The top-level `model` must use `provider-id/model-id` format. It is the **main** chat model.

Optional top-level `models` maps roles to other catalog refs (ADR-045). Unset roles fall back to `model`:

```json
{
  "model": "deepseek/deepseek-chat",
  "models": {
    "subagent": "deepseek/deepseek-chat",
    "compact": "openai/gpt-cheap"
  }
}
```

Allowed keys: `subagent` (`task` child runs), `compact` (session head summarization). Do not set `models.main`. TUI `/model` only rewrites top-level `model`; changing `models.*` requires a daemon restart. Unknown keys or refs outside the catalog fail config load.

## Protocol families and aliases

| Canonical protocol | Accepted `type` / `protocol` aliases | Default completion endpoint | Default API-key header |
| --- | --- | --- | --- |
| `openai-chat` | `openai-compatible`, `openai-compat`, `openai-chat-completions`, `chat-completions`, `ollama`, `lmstudio`, `llamacpp`, `vllm`, `litellm` | `/v1/chat/completions` | `Authorization: Bearer ...` |
| `openai-responses` | `openai`, `responses` | `/v1/responses` | `Authorization: Bearer ...` |
| `anthropic-messages` | `anthropic`, `anthropic-compatible`, `claude` | `/v1/messages` | `x-api-key: ...` |
| `gemini-generate-content` | `gemini`, `google`, `google-generative-ai` | `/v1beta/models/{model}:generateContent` | `x-goog-api-key: ...` |

If neither field is present, AutoZeAgent keeps backward compatibility by selecting `openai-chat`. If both `type` and `protocol` are present, they must resolve to the same canonical protocol.

Use `openai-compatible` for providers that expose Chat Completions. The `openai` alias intentionally selects the newer OpenAI Responses protocol.

## Common options

```json
{
  "options": {
    "baseURL": "https://gateway.example/v1",
    "apiKey": "{env:PROVIDER_API_KEY}",
    "completionPath": "/v1/chat/completions?api-version=2026-01-01",
    "modelsPath": "/v1/models",
    "headers": {
      "X-Organization": "{env:PROVIDER_ORG}",
      "api-key": "{file:.secrets/azure-key}"
    }
  }
}
```

- `baseURL` must be an absolute HTTP(S) URL without query parameters, fragments, or userinfo. A path prefix is allowed.
- `completionPath` and `modelsPath` must begin with `/` and may contain query parameters.
- If `baseURL` already ends in `/v1`, the default `/v1/...` endpoint does not duplicate that segment.
- `headers` are applied after protocol defaults, so an explicitly configured header can override a default authorization or version header.
- `apiKey` and every header value accept literal values, `{env:NAME}`, or `{file:path}`. Relative file paths are resolved from the JSON file's directory.
- `responseFormat` configures structured-output behavior only for `openai-chat`. It is optional; omit it to use automatic negotiation. `auto`, `json_schema`, and `json_object` are accepted as explicit values.
- `anthropicVersion` defaults to `2023-06-01`.
- Gemini `completionPath` must contain `{model}` because the model ID is part of the request URL.

## OpenAI-compatible Chat Completions

```json
{
  "model": "local/qwen3",
  "provider": {
    "local": {
      "type": "openai-compatible",
      "options": {
        "baseURL": "http://127.0.0.1:11434/v1"
      },
      "models": {
        "qwen3": { "name": "Qwen 3" }
      }
    }
  }
}
```

This protocol family covers OpenAI-compatible services such as DeepSeek, OpenRouter, Ollama, LM Studio, llama.cpp, vLLM, and LiteLLM when they expose `/chat/completions` semantics.

When a request includes a JSON Schema and `responseFormat` is omitted (or set to `auto`), AutoZeAgent first sends OpenAI's `json_schema` format. If the endpoint explicitly reports that `response_format` or `json_schema` is unsupported, AutoZeAgent retries once with `json_object` and remembers that choice for subsequent requests to the same model. Set `responseFormat` explicitly only when a gateway needs a fixed compatibility override or when debugging negotiation.

## Per-model generation options

Generation options belong to each entry in `models`, so models sharing a provider URL and API key can still use different defaults:

```json
{
  "models": {
    "chat-model": {
      "name": "Chat model",
      "temperature": 0.2,
      "maxTokens": 4096,
      "reasoningEffort": "high"
    }
  }
}
```

- `temperature` is used when the request does not already specify a temperature.
- `maxTokens` is a model-level output-token cap. It fills an unset request limit, caps a larger request limit, and preserves a smaller request limit.
- `contextWindow` is the model context length in tokens (optional). It is **not** the same as `maxTokens` (output cap). Used for **provider-view packing and TUI pressure** (ADR-041): usable window ≈ `contextWindow − maxOutput − reserve`. Omit or `0` when unknown (packing falls back to L1 trim only); do not copy run budgets into this field.
- `reasoningEffort` is used when the request does not already specify an effort. It currently maps to `reasoning_effort` for OpenAI-compatible Chat Completions and to `reasoning.effort` for OpenAI Responses.
- Request-level values take precedence over model defaults, except that `maxTokens` always remains an upper bound.
- Configure only options supported by the selected model and endpoint. AutoZeAgent rejects `reasoningEffort` for the Anthropic and Gemini adapters rather than silently ignoring it.

## OpenAI Responses

```json
{
  "model": "openai/gpt-model-id",
  "provider": {
    "openai": {
      "type": "openai",
      "options": {
        "baseURL": "https://api.openai.com",
        "apiKey": "{env:OPENAI_API_KEY}"
      },
      "models": {
        "gpt-model-id": { "name": "OpenAI model" }
      }
    }
  }
}
```

## Anthropic Messages

```json
{
  "model": "anthropic/claude-model-id",
  "provider": {
    "anthropic": {
      "type": "anthropic",
      "options": {
        "baseURL": "https://api.anthropic.com",
        "apiKey": "{env:ANTHROPIC_API_KEY}",
        "anthropicVersion": "2023-06-01"
      },
      "models": {
        "claude-model-id": { "name": "Claude model" }
      }
    }
  }
}
```

## Google Gemini generateContent

```json
{
  "model": "google/gemini-model-id",
  "provider": {
    "google": {
      "type": "gemini",
      "options": {
        "baseURL": "https://generativelanguage.googleapis.com",
        "apiKey": "{env:GEMINI_API_KEY}"
      },
      "models": {
        "gemini-model-id": { "name": "Gemini model" }
      }
    }
  }
}
```

## Azure/OpenAI-compatible gateways

Use a custom endpoint query and header when the gateway does not accept bearer authentication:

```json
{
  "model": "azure/deployment-model-id",
  "provider": {
    "azure": {
      "type": "openai-compatible",
      "options": {
        "baseURL": "https://resource.openai.azure.com",
        "completionPath": "/openai/deployments/deployment-name/chat/completions?api-version=2026-01-01",
        "modelsPath": "/openai/models?api-version=2026-01-01",
        "headers": {
          "api-key": "{env:AZURE_OPENAI_API_KEY}"
        }
      },
      "models": {
        "deployment-model-id": { "name": "Azure deployment" }
      }
    }
  }
}
```

Do not also set `apiKey` when the service rejects the protocol's default authorization header; put the secret in `headers` instead.
