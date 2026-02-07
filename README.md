# squidbot

Go-native personal AI assistant with Telegram integration, actor-based session runtime, and BoltDB persistence.

## Features

- Session actor runtime with bounded mailboxes and idle eviction
- BoltDB primary storage (`~/.squidbot/data/squidbot.db`)
- Tool loop with typed tool argument boundaries
- Provider adapters for OpenRouter/OpenAI and Anthropic
- Telegram channel adapter (polling)
- Cron scheduler and heartbeat service
- Structured runtime metrics counters
- Legacy import command (`squidbot migrate --from-legacy-home ~/.nanobot`)

## Install

```bash
go build -o squidbot ./cmd/squidbot
```

## Quick Start

1. Initialize config and workspace:
```bash
./squidbot onboard
```

2. Set your provider API key in `~/.squidbot/config.json`.

3. Run direct chat:
```bash
./squidbot agent -m "Hello"
```

4. Start gateway:
```bash
./squidbot gateway
```

## Migrate From Legacy

```bash
./squidbot migrate --from-legacy-home ~/.nanobot
```

This imports legacy sessions, cron jobs, and workspace files. By default it also merges missing config fields from the legacy config.

## CLI

- `squidbot onboard`
- `squidbot status`
- `squidbot agent -m "..."`
- `squidbot agent`
- `squidbot gateway`
- `squidbot telegram status`
- `squidbot cron list|add|remove|enable|run`
- `squidbot doctor`
- `squidbot migrate --from-legacy-home ~/.nanobot`

## Testing

```bash
go test ./...
go test -race ./...
```
