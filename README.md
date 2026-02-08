# squidbot

Go-native personal AI assistant with Telegram integration, actor-based session runtime, and BoltDB persistence.

## Features

- Session actor runtime with bounded mailboxes and idle eviction
- BoltDB primary storage (`~/.squidbot/data/squidbot.db`)
- Tool loop with typed tool argument boundaries
- Provider adapters for OpenRouter, Anthropic, OpenAI, Gemini, Ollama, and LM Studio
- Mandatory provider-gated onboarding before runtime commands
- Telegram channel adapter (polling)
- Cron scheduler and heartbeat service
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

3. Run direct chat:
```bash
./squidbot agent -m "Hello"
```

4. Start gateway:
```bash
./squidbot gateway
```

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
- `squidbot status`
- `squidbot agent -m "..."`
- `squidbot agent`
- `squidbot gateway`
- `squidbot telegram status`
- `squidbot cron list|add|remove|enable|run`
- `squidbot doctor`

## Testing

```bash
go test ./...
go test -race ./...
```
