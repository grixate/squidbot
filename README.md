# squidbot

Go-native personal AI assistant with Telegram integration, actor-based session runtime, BoltDB persistence, and a SQLite memory index sidecar.

## Features

- Session actor runtime with bounded mailboxes and idle eviction
- BoltDB primary storage (`~/.squidbot/data/squidbot.db`)
- SQLite memory index for markdown-based retrieval (`~/.squidbot/data/memory_index.db`)
- Tool loop with typed tool argument boundaries
- Provider adapters for OpenRouter, Anthropic, OpenAI, Gemini, Ollama, and LM Studio
- Mandatory provider-gated onboarding before runtime commands
- Browser onboarding and management UI server (`squidbot manage`)
- Telegram channel adapter (polling)
- Cron scheduler and heartbeat service
- Auto-discovered skill contracts from `skills/**/SKILL.md`
- Episodic daily memory logs in `memory/daily/YYYY-MM-DD.md`
- Structured runtime metrics counters

## Install

```bash
go build -o squidbot ./cmd/squidbot
```

## Quick Start

1. Initialize config and workspace:
```bash
./squidbot onboard
```

2. Follow CLI prompts to choose provider and enter credentials/model.
   Onboarding also supports optional Telegram setup (enable flag, bot token, allow list).
   For Gemini you can optionally verify Gemini CLI connectivity during onboarding.
   You can choose terminal onboarding or browser onboarding (`--mode web`).

3. Run direct chat:
```bash
./squidbot agent -m "Hello"
```

4. Start gateway:
```bash
./squidbot gateway
```

## Workspace Contract

Onboarding scaffolds these markdown files in your workspace:

- `AGENTS.md`
- `SOUL.md`
- `USER.md`
- `TOOLS.md`
- `HEARTBEAT.md`
- `memory/MEMORY.md`
- `memory/daily/` (episodic logs)
- `skills/README.md` and optional `skills/**/SKILL.md`

Prompt assembly includes:

- bootstrap markdown (`AGENTS.md`, `SOUL.md`, `USER.md`, `TOOLS.md`)
- curated memory (`memory/MEMORY.md`)
- indexed retrieval snippets from memory markdown files
- recent daily memory snippets
- discovered skill summaries

## Non-Interactive Onboarding

Example: Gemini

```bash
./squidbot onboard --non-interactive --provider gemini --api-key "$SQUIDBOT_GEMINI_API_KEY" --model gemini-3.0-pro --verify-gemini-cli --telegram-enabled --telegram-token "$SQUIDBOT_TELEGRAM_TOKEN" --telegram-allow-from 123456789 --telegram-allow-from @my_username
```

Example: Ollama

```bash
./squidbot onboard --non-interactive --provider ollama --model llama3.1:8b --api-base http://localhost:11434/v1
```

Example: LM Studio

```bash
./squidbot onboard --non-interactive --provider lmstudio --model local-model --api-base http://localhost:1234/v1
```

Telegram flags for non-interactive onboarding:

- `--telegram-enabled` (must include `--telegram-token` when true)
- `--telegram-token <bot_token>`
- `--telegram-allow-from <id_or_username>` (repeatable, comma-separated values also supported)

## CLI

- `squidbot onboard`
- `squidbot manage`
- `squidbot status`
- `squidbot agent -m "..."`
- `squidbot agent`
- `squidbot gateway`
- `squidbot telegram status`
- `squidbot cron list|add|remove|enable|run`
- `squidbot doctor`

Gateway can also host management UI/API when started with:

```bash
./squidbot gateway --with-manage
```

## Testing

```bash
go test ./...
go test -race ./...
```
