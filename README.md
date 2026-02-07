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
./squidbot onboard --non-interactive --provider gemini --api-key "$SQUIDBOT_GEMINI_API_KEY" --model gemini-3.0-pro --verify-gemini-cli
```

Example: Ollama

```bash
./squidbot onboard --non-interactive --provider ollama --model llama3.1:8b --api-base http://localhost:11434/v1
```

Example: LM Studio

```bash
./squidbot onboard --non-interactive --provider lmstudio --model local-model --api-base http://localhost:1234/v1
```

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
