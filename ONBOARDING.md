# Onboarding

Provider setup is mandatory before runtime commands (`agent`, `gateway`, `cron run`).

## Interactive Setup

```bash
./squidbot onboard
```

The CLI will ask you to:
- choose a provider
- enter required credentials
- set API base/model defaults
- optionally configure Telegram (`enabled`, `token`, allow list)
- optionally verify Gemini CLI connectivity for Gemini

Onboarding also ensures workspace scaffolding for:

- `AGENTS.md`, `SOUL.md`, `USER.md`, `TOOLS.md`, `HEARTBEAT.md`
- `memory/MEMORY.md`
- `memory/daily/` (episodic logs)
- `skills/README.md` (drop custom `skills/**/SKILL.md` contracts here)

Default providers include OpenRouter, Anthropic, OpenAI, OpenAI Codex (OAuth), Gemini, Ollama, LM Studio, Moonshot AI, and MiniMax.

## Non-Interactive Setup

Gemini:

```bash
./squidbot onboard --non-interactive --provider gemini --api-key-ref env:SQUIDBOT_GEMINI_API_KEY --model gemini-3.0-pro --verify-gemini-cli --telegram-enabled --telegram-token-ref env:SQUIDBOT_TELEGRAM_TOKEN --telegram-allow-from 123456789 --telegram-allow-from @my_username
```

Ollama:

```bash
./squidbot onboard --non-interactive --provider ollama --model llama3.1:8b --api-base http://localhost:11434/v1
```

LM Studio:

```bash
./squidbot onboard --non-interactive --provider lmstudio --model local-model --api-base http://localhost:1234/v1
```

Moonshot AI:

```bash
./squidbot onboard --non-interactive --provider moonshot --api-key-ref env:SQUIDBOT_PROVIDER_MOONSHOT_API_KEY
```

MiniMax:

```bash
./squidbot onboard --non-interactive --provider minimax --api-key-ref env:SQUIDBOT_PROVIDER_MINIMAX_API_KEY
```

OpenAI Codex (OAuth):

```bash
./squidbot onboard --non-interactive --provider openai-codex --model openai-codex/gpt-5.1-codex
./squidbot provider login openai-codex
```

Telegram flags:

- `--telegram-enabled` (requires `--telegram-token-ref` when enabled)
- `--telegram-token-ref <env:VAR|file:/abs/path|systemd:name>`
- `--telegram-allow-from <id_or_username>` (repeatable, comma-separated supported)

MCP flags are configured in `config.json` (not via onboard flags yet):

```json
{
  "features": {
    "mcp": true
  },
  "tools": {
    "mcp": {
      "enabled": true,
      "connectTimeoutSec": 20,
      "servers": {
        "filesystem": {
          "enabled": true,
          "command": "npx",
          "args": ["-y", "@modelcontextprotocol/server-filesystem", "/path/to/workspace"]
        }
      }
    }
  }
}
```

## Secret Migration (Hard Cutover)

If an existing `config.json` still contains plaintext secret values (`apiKey`, `token`, `authToken`), runtime load now fails closed.
Use:

```bash
./squidbot secrets migrate --output-dir /etc/squidbot/credentials
```

This command writes secret files with mode `0400` and rewrites config entries to `*Ref` fields using `file:/...` references.

## Verify Setup

```bash
./squidbot status
./squidbot doctor
```

`doctor` now validates:

- active provider readiness
- Telegram token consistency when Telegram is enabled
- required markdown workspace files
- heartbeat file readability
- memory index accessibility
- skill runtime summary (total/valid/invalid/zip counts + warnings)

If provider setup is incomplete, runtime commands will fail with:

```text
provider setup incomplete ... Run `squidbot onboard`
```

For OpenAI Codex, this usually means OAuth login has not been completed yet:

```bash
./squidbot provider login openai-codex
```
