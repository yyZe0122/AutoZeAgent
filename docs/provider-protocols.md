# Provider protocol configuration

YunmengZe selects a wire protocol from each provider's JSON configuration. The provider ID and URL do not imply a protocol, so the same gateway URL can expose different adapters by changing only `type` or `protocol`.

Configuration is loaded **only** from the OS config directory (`paths.Layout.ConfigDir`):

1. `<config-dir>/agent.local.json` (machine-local; preferred)
2. `<config-dir>/agent.json`
3. Optional `<config-dir>/env` — `KEY=value` lines loaded into the process before resolving `{env:…}` (does **not** override variables already set in the environment)

| Mode | Path |
| --- | --- |
| user (all OS) | `~/.yunmengze` (`%USERPROFILE%\.yunmengze` on Windows); override with `YMZ_HOME` |
| system | Linux `/etc/yunmengze` · Windows `ProgramData\YunmengZe\config` · macOS system path from `paths` |

On first start, if ConfigDir has no file, the daemon may **migrate** once from the process working directory or data dir (legacy project `agent.local.json`), otherwise it writes a default template with `{env:…}` placeholders (no secrets) and may seed an empty `env` template. Installers do the same without overwriting existing files. Project directories are **not** searched for ongoing loads.

## API keys (choose any; nothing is forced)

`options.apiKey` supports three forms:

| Form | Example | Notes |
| --- | --- | --- |
| Environment placeholder | `"{env:DEEPSEEK1_API_KEY}"` | **Recommended.** Value from process env and/or ConfigDir `env` file |
| File reference | `"{file:secrets/key.txt}"` | Path relative to ConfigDir, or absolute |
| Literal string | `"sk-..."` | Allowed for local convenience; protect file permissions; never commit |

Examples:

```json
"apiKey": "{env:DEEPSEEK1_API_KEY}"
```

```json
"apiKey": "{file:secrets/deepseek.key}"
```

```json
"apiKey": "sk-your-key-here"
```

Optional ConfigDir `env` file:

```bash
# ~/.yunmengze/env  (chmod 600)
DEEPSEEK1_API_KEY=sk-...
DEEPSEEK2_API_KEY=sk-...
```

See also [`configs/agent.json.example`](../configs/agent.json.example) (includes env / file / literal illustrations).

## Multi-provider catalog (OpenCode-style nesting)

Each entry under `provider` is one **supplier** (endpoint + wire protocol + credentials). Its **model catalog is nested** under that entry.

Selection and wire ids follow **OpenCode** rules:

1. Top-level `model` is `providerID/modelID…` — only the **first** `/` separates supplier from model segment.
2. The model segment **may contain `/`** (OpenRouter / NewAPI style: `deepseek/deepseek-v4-flash`).
3. Catalog keys under `provider.<id>.models` must equal that model segment (exact match; may contain `/`).
4. The HTTP body `model` field is the **wire id**: `models.<key>.id` if set, otherwise the model segment (never the full selection string, never a stripped-down bare rewrite of a nested id).

| Concept | JSON path | Example |
| --- | --- | --- |
| Active selection | top-level `model` | `"deepseek1/deepseek-chat"` or `"deepseek2/deepseek/deepseek-v4-flash"` |
| Supplier deepseek1 | `provider.deepseek1` | official bare model ids |
| Supplier deepseek2 | `provider.deepseek2` | gateway nested wire ids / `id` override |
| Catalog key | `provider.<id>.models` | `"deepseek-chat"` or `"deepseek/deepseek-v4-flash"` |
| Wire override | `models.<key>.id` | `"flash": { "id": "deepseek/deepseek-v4-flash" }` |
| Role overrides | top-level `models` | `subagent` / `compact` → selection ref (ADR-045) — **not** the catalog |

Same model segment on two suppliers is fine; selection disambiguates. Templates use **`deepseek1`** / **`deepseek2`**:

```json
{
  "model": "deepseek1/deepseek-chat",
  "provider": {
    "deepseek1": {
      "type": "openai-compatible",
      "options": { "baseURL": "https://api.deepseek.com/v1", "apiKey": "{env:DEEPSEEK1_API_KEY}" },
      "models": { "deepseek-chat": { "name": "DeepSeek Chat" } }
    },
    "deepseek2": {
      "type": "openai-compatible",
      "options": { "baseURL": "https://llm.example.com/v1", "apiKey": "{env:DEEPSEEK2_API_KEY}" },
      "models": {
        "deepseek/deepseek-v4-flash": { "name": "Nested wire id" },
        "flash": { "name": "Flash alias", "id": "deepseek/deepseek-v4-flash" }
      }
    }
  }
}
```

- Prefer catalog keys equal to the upstream model id (including `/` when the gateway requires it). Optional `id` overrides the wire name while keeping a short selection key.
- Mistyped keys of the form `providerID/modelID` when the selection model segment is bare are still accepted as a convenience; wire id remains the bare segment (or `id`).
- Empty `models` under a provider allows any model id (pass-through; API may still reject unknown ids).
- TUI `/model` lists `providerId/modelId…` refs and only changes top-level `model`.
- **`ready`:** true only when config load succeeded **and** agent/chat was bound at daemon start. Otherwise `error` explains (fix config, or `ymz restart` if chat never started). Secrets never appear in `error`.

### Hot-reload (ADR-048)

While `ymzd` runs, edits to `agent.json` / `agent.local.json` / `env` rebuild the **main** provider client after ~500ms (`internal/providerruntime`). Fingerprint includes a hash of API key and headers (not plaintext in logs).

| Change | Hot-reload? |
| --- | --- |
| `model`, baseURL, protocol, maxTokens, contextWindow | Yes |
| Literal `apiKey` or `{file:…}` content | Yes |
| `{env:VAR}` via `env` file when process VAR is empty | Yes |
| Process env already set for `{env:VAR}` | **No** — change process env + restart |
| `chat.*`, MCP, `models.subagent\|compact` | **No** — `ymz restart` |
| Daemon started without agent (bad config) | Fix file then **`ymz restart`** (no late-bind) |

In-flight runs keep the previous client until the next turn.

### Role map (optional)

Top-level `models` maps roles to other **selection** refs (ADR-045). Unset roles fall back to `model`:

```json
{
  "model": "deepseek1/deepseek-chat",
  "models": {
    "subagent": "deepseek2/flash",
    "compact": "deepseek2/flash"
  }
}
```

Allowed keys: `subagent` (`task` child runs), `compact` (session head summarization). Do not set `models.main`. Changing `models.*` requires a daemon restart.

## Protocol families and aliases

| Canonical protocol | Accepted `type` / `protocol` aliases | Default completion endpoint | Default API-key header |
| --- | --- | --- | --- |
| `openai-chat` | `openai-compatible`, `openai-compat`, `openai-chat-completions`, `chat-completions`, `ollama`, `lmstudio`, `llamacpp`, `vllm`, `litellm` | `/v1/chat/completions` | `Authorization: Bearer ...` |
| `openai-responses` | `openai`, `responses` | `/v1/responses` | `Authorization: Bearer ...` |
| `anthropic-messages` | `anthropic`, `anthropic-compatible`, `claude` | `/v1/messages` | `x-api-key: ...` |
| `gemini-generate-content` | `gemini`, `google`, `google-generative-ai` | `/v1beta/models/{model}:generateContent` | `x-goog-api-key: ...` |

If neither field is present, YunmengZe keeps backward compatibility by selecting `openai-chat`. If both `type` and `protocol` are present, they must resolve to the same canonical protocol.

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

When a request includes a JSON Schema and `responseFormat` is omitted (or set to `auto`), YunmengZe first sends OpenAI's `json_schema` format. If the endpoint explicitly reports that `response_format` or `json_schema` is unsupported, YunmengZe retries once with `json_object` and remembers that choice for subsequent requests to the same model. Set `responseFormat` explicitly only when a gateway needs a fixed compatibility override or when debugging negotiation.

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
- Configure only options supported by the selected model and endpoint. YunmengZe rejects `reasoningEffort` for the Anthropic and Gemini adapters rather than silently ignoring it.

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
