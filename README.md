# squidbot

Reliability-first personal AI assistant in Go, with actor-isolated sessions, durable BoltDB runtime state, and markdown memory indexed in SQLite.

## What Squidbot Optimizes For

- Correctness and recoverability over feature sprawl
- Durable state over ad-hoc flat-file persistence
- Predictable session behavior under concurrency
- Human-editable memory plus indexed retrieval

Current channel scope is intentionally narrow (Telegram + CLI) while core runtime quality is prioritized.

## Highlights

- Actor-based session runtime with bounded mailboxes and idle eviction
- BoltDB primary store (`~/.squidbot/data/squidbot.db`) for turns, jobs, checkpoints, usage, and operational events
- SQLite memory index (`~/.squidbot/data/memory_index.db`) with FTS5 over workspace memory markdown
- Hybrid memory context in prompts: curated memory + retrieved chunks + recent daily notes
- Cron scheduler, heartbeat service, and structured run/event recording
- Provider adapters: OpenRouter, Anthropic, OpenAI, Gemini, Ollama, LM Studio
- Skills discovery from `skills/**/SKILL.md`
- Doctor checks for provider readiness, workspace contract, memory index, and skills availability

## Architecture At A Glance

1. Inbound message enters session actor (`session_id` keyed).
2. Actor loads bounded conversation window from BoltDB.
3. System prompt is assembled from bootstrap files + memory retrieval.
4. LLM/tool loop executes with workspace-constrained tools.
5. Turns, tool events, and metadata are persisted.
6. Daily episodic memory is appended and re-indexed.

## Persistence Model

| Layer | Location | Purpose |
| --- | --- | --- |
| Runtime DB | `~/.squidbot/data/squidbot.db` | Sessions, turns, tool events, cron jobs/runs, checkpoints, mission data, usage, heartbeat runs |
| Memory source | `<workspace>/memory/MEMORY.md`, `<workspace>/memory/daily/*.md` | Human-editable long-term and episodic memory |
| Memory index | `~/.squidbot/data/memory_index.db` | Chunk index + FTS retrieval for prompt-time recall |

## Install

```bash
go build -o squidbot ./cmd/squidbot
```

## Quick Start

1. Initialize config and workspace:

```bash
./squidbot onboard
```

2. Chat directly:

```bash
./squidbot agent -m "Hello"
```

3. Run gateway (Telegram + background services):

```bash
./squidbot gateway
```

4. Validate environment health:

```bash
./squidbot doctor
```

## Isolated Local Testing

Use the wrapper to keep config/data/workspace local to this clone instead of `~/.squidbot`:

```bash
./scripts/dev-squidbot.sh onboard
./scripts/dev-squidbot.sh gateway
./scripts/dev-squidbot.sh status
```

Helpers:

```bash
./scripts/dev-squidbot.sh where
./scripts/dev-squidbot.sh reset
```

Change isolated root:

```bash
SQUIDBOT_DEV_HOME=/tmp/squidbot-dev ./scripts/dev-squidbot.sh status
```

## Workspace Contract

Onboarding scaffolds:

- `AGENTS.md`
- `SOUL.md`
- `USER.md`
- `TOOLS.md`
- `HEARTBEAT.md`
- `memory/MEMORY.md`
- `memory/daily/`
- `skills/README.md`

Prompt assembly includes:

- bootstrap markdown (`AGENTS.md`, `SOUL.md`, `USER.md`, `TOOLS.md`)
- curated memory (`memory/MEMORY.md`)
- retrieved memory chunks from indexed markdown
- recent daily memory snippets
- discovered skill summaries

## Memory Behavior

- `memory/MEMORY.md` is curated long-term memory.
- `memory/daily/YYYY-MM-DD.md` receives structured episodic entries after conversations and heartbeat runs.
- Daily logs are retention-pruned (default 90 days).
- Memory index sync reconciles chunks to source files (upsert current, delete stale).
- Retrieval is lexical-first (FTS/LIKE) plus recency weighting.

## Non-Interactive Onboarding

Gemini:

```bash
./squidbot onboard --non-interactive --provider gemini --api-key "$SQUIDBOT_GEMINI_API_KEY" --model gemini-3.0-pro --verify-gemini-cli --telegram-enabled --telegram-token "$SQUIDBOT_TELEGRAM_TOKEN" --telegram-allow-from 123456789 --telegram-allow-from @my_username
```

Ollama:

```bash
./squidbot onboard --non-interactive --provider ollama --model llama3.1:8b --api-base http://localhost:11434/v1
```

LM Studio:

```bash
./squidbot onboard --non-interactive --provider lmstudio --model local-model --api-base http://localhost:1234/v1
```

Telegram flags:

- `--telegram-enabled` (requires token)
- `--telegram-token <bot_token>`
- `--telegram-allow-from <id_or_username>` (repeatable; comma-separated also supported)

## CLI Reference

- `squidbot onboard`
- `squidbot status`
- `squidbot agent -m "..."`
- `squidbot agent`
- `squidbot gateway`
- `squidbot telegram status`
- `squidbot cron list --all`
- `squidbot cron add --name ... --message ... --every <seconds>`
- `squidbot cron add --name ... --message ... --cron "<expr>"`
- `squidbot cron add --name ... --message ... --at <RFC3339>`
- `squidbot cron remove <job_id>`
- `squidbot cron enable <job_id> [--disable]`
- `squidbot cron run <job_id> [--force]`
- `squidbot doctor`

## Branch Policy

- `main`: core Go/CLI runtime only
- `squidbot-ui`: visual onboarding and management stack

## Testing

```bash
go test ./...
go test -race ./...
```
