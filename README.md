# Squidbot 🦑

**Reliability-first personal AI assistant in Go.**  
Built for long-running operation with durable state, editable memory, subagents, and operations-grade safety.

Squidbot is not a demo bot. It is an assistant runtime designed to behave like dependable infrastructure.

---

## Why Squidbot?

Most OpenClaw assistants optimize for fast demos and feature breadth.  
Squidbot optimizes for **predictability, durability, and control**.

If you want an assistant that can run continuously, remember what happened yesterday, delegate work safely, and fail loudly instead of silently, Squidbot is built for that job.

**Design priorities:**

- Reliability over feature sprawl
- Explicit state over implicit magic
- Human-inspectable memory
- Operations-grade safety and observability

---

## Key Differentiators

### 🧠 Durable Runtime, Not a Stateless Wrapper

Squidbot persists *everything that matters*.

- Actor-isolated sessions with bounded mailboxes
- Explicit backpressure (`ErrMailboxFull`) instead of silent overload
- Crash-safe checkpointing per session
- Idle actor eviction for predictable resource usage

**Outcome:** predictable behavior under concurrency and long runtimes.

---

### 💾 Durable-by-Default State Model

All core activity is recorded in BoltDB:

- Turns and tool events
- Jobs, job runs, and cron executions
- Checkpoints and missions
- Subagent and federation events
- Token budgets and usage accounting

A single-writer queue ensures consistency and avoids corruption.

**Outcome:** Squidbot behaves like operational software, not a transient chat loop.

---

### 📝 Human-Editable Memory with Fast Retrieval

Memory is not a black box.

- Long-term memory: `memory/MEMORY.md`
- Episodic memory: `memory/daily/*.md`
- SQLite index with full-text search
- Optional semantic reranking with guaranteed lexical fallback
- Automatic reconciliation when files change

**Outcome:** you can read, edit, version, and trust your assistant's memory.

---

### 🤖 Subagents & Multi-Agent-Ready Architecture

Squidbot is designed for delegation.

- Explicit subagent lifecycle management
- New **Channel-Branch-Worker** runtime split:
  - `Channel`: user-facing session actor loop
  - `Branch`: lightweight reasoning fork (memory-first, limited tools)
  - `Worker`: execution path (existing subagent + federation flow)
- Isolated memory and budgets per agent
- Federation HTTP server/client for trusted peer execution
- Idempotency keys and peer health tracking

**Outcome:** scale from a single assistant to coordinated agents without rewrites.

---

### 🔐 Safety & Budget Guardrails (First-Class)

Safety is not an afterthought.

- Token preflight reservations
- Hard limits and soft warnings
- Budget accounting per session and agent
- Safety events recorded in the runtime ledger

**Outcome:** predictable cost and behavior, even under automation.

---

### ⚙️ Automation Beyond Chat

Squidbot supports autonomous workflows.

- Cron service (interval, cron expression, or fixed time)
- Heartbeat service with persisted run history
- Deliver-back options for completed jobs
- Restart-safe execution tracking

**Outcome:** assistants that actually do things while you are away.

---

### 🔌 Multi-Provider & OpenClaw-Compatible

Avoid provider lock-in.

- Process-aware model routing (channel/branch/worker/compactor/cortex)
- OpenClaw catalog parity support
- Out-of-box providers: OpenRouter, Anthropic, OpenAI, Gemini, Ollama, LM Studio, Moonshot AI, MiniMax
- Multiple provider and channel profiles
- Per-process fallback model chains with cooldown for rate-limit pressure

**Outcome:** portability without sacrificing capability.

---

### 📡 Channels with Built-In Fallbacks

- Native adapters: Telegram, Slack, Discord, WebChat, WhatsApp
- Webhook / noop fallback for additional channels

**Outcome:** high uptime even when integrations fail.

---

### 🩺 Operations-Grade Tooling

- `doctor` command for full system health checks
- Prometheus-style metrics endpoint
- Bearer token and localhost access controls

**Outcome:** visibility and confidence in production-like environments.

---

## 60-Second Quick Start

```bash
go build -o squidbot ./cmd/squidbot
./squidbot onboard
./squidbot agent -m "Hello"
./squidbot gateway
./squidbot doctor
```

---

## Local Isolated Dev Mode

Run without touching `~/.squidbot`:

```bash
./scripts/dev-squidbot.sh onboard
./scripts/dev-squidbot.sh gateway
./scripts/dev-squidbot.sh status
./scripts/dev-squidbot.sh where
```

---

## Architecture at a Glance

1. User message enters a session `Channel` actor turn
2. Channel decides direct reply vs `Branch` reasoning vs `Worker` execution
3. Prompt assembles history + memory + skills + cortex bulletin
4. Tool/model loop runs with process-aware routing + fallbacks
5. Worker results flow via existing subagent/federation lifecycle
6. Background compactor trims old turns and injects summary markers
7. Cortex periodically refreshes bulletin from memory slices
8. Runs/events/usage are persisted for management observability

---

## Runtime Topology

### Channel

- Per-session actor loop and user-facing orchestration
- Full tool registry, task automation, and outbound messaging

### Branch

- Ephemeral reasoning process with constrained toolset:
  - `memory_recall`
  - `memory_save`
  - `memory_delete`
  - `channel_recall`
- Spawned via channel tools:
  - `branch_spawn`
  - `branch_status`
  - `branch_wait`

### Worker

- Existing subagent pipeline (`spawn`, `subagent_wait`, `subagent_status`, etc.)
- Supports local / remote / auto routing through federation
- Keeps artifact and execution-oriented behavior unchanged

---

## Long-Run Conversation Hygiene

### Background Compactor

- Monitors context pressure after turns
- Threshold actions:
  - background compaction
  - aggressive compaction
  - emergency truncate (no LLM dependency)
- Removes oldest turns, stores a compact summary marker
- Guarantees one active compaction run per session

### Cortex Bulletin

- Periodic memory synthesis into a short operational bulletin
- Injected into channel prompt each turn
- Retains previous bulletin when generation fails

---

## New Runtime Config Blocks

Under `runtime`:

- `routing`:
  - `channelModel`, `branchModel`, `workerModel`, `compactorModel`, `cortexModel`
  - `taskOverrides`, `fallbacks`, `rateLimitCooldownSec`
- `compaction`:
  - `enabled`, `backgroundThresholdPct`, `aggressiveThresholdPct`, `emergencyThresholdPct`
- `cortex`:
  - `enabled`, `bulletinIntervalSec`, `bulletinMaxWords`

Defaults are conservative and can be tuned incrementally.

---

## Management Surfaces (Board / API)

Runtime observability endpoints:

- `GET /api/manage/runtime/branches`
- `GET /api/manage/runtime/compaction/runs`
- `GET /api/manage/runtime/cortex/events`

Memory bulletin endpoints:

- `GET /api/manage/memory/bulletin`
- `POST /api/manage/memory/bulletin/regenerate`

Settings endpoints:

- `GET/PUT /api/manage/settings/routing`
- `GET/PUT /api/manage/settings/compaction`
- `GET/PUT /api/manage/settings/cortex`

---

## Adaptive Context Control (Optional)

When `contextControl.enabled` is true, Squidbot can adapt prompt assembly for smaller context windows:

- model-window lookup from `workspace/.squidbot/model-windows.json`
- staged compression at configured thresholds
- optional session-summary persistence for long threads

Model window registry format:

```json
{
  "version": 1,
  "defaults": {
    "contextWindowTokens": 8192,
    "outputReserveTokens": 1024,
    "charsPerToken": 4.0
  },
  "models": [
    {
      "name": "gemma3:4b",
      "aliases": ["gemma-3-4b", "google/gemma-3-4b-it"],
      "contextWindowTokens": 8192,
      "outputReserveTokens": 1024,
      "charsPerToken": 3.6
    }
  ]
}
```

The file is checked every request and reloaded when its mtime changes.

---

## Persistence Model

| Layer | Location | Purpose |
| --- | --- | --- |
| Runtime DB | `~/.squidbot/data/squidbot.db` | Sessions, turns, tools, jobs, checkpoints, budgets |
| Memory source | `memory/MEMORY.md`, `memory/daily/*.md` | Human-editable long-term and episodic memory |
| Memory index | `~/.squidbot/data/memory_index.db` | Fast retrieval and optional semantic reranking |

---

## Workspace Contract

Onboarding scaffolds a clear, explicit workspace:

- `AGENTS.md`
- `SOUL.md`
- `USER.md`
- `TOOLS.md`
- `HEARTBEAT.md`
- `memory/MEMORY.md`
- `memory/daily/`
- `skills/README.md`

---

## Common Commands

```bash
squidbot onboard
squidbot status
squidbot agent -m "..."
squidbot gateway
squidbot doctor

squidbot cron list --all
squidbot cron add --name ... --message ... --every <seconds>
squidbot cron add --name ... --message ... --cron "<expr>"
squidbot cron add --name ... --message ... --at <RFC3339>
squidbot cron run <job_id>
squidbot cron remove <job_id>

squidbot skills list
squidbot skills check
squidbot skills reload

squidbot budget status
```

---

## Runtime Status

The following runtime evolution work is now integrated:

- Channel-Branch-Worker process split
- Background context compaction service
- Process-aware model routing + fallbacks
- Cortex bulletin generation and prompt injection
- Management APIs/UI for runtime observability and settings

---

## Testing & Quality

```bash
go test ./...
go test -race ./...
```

40+ test files cover the runtime core.

Outcome: confidence under concurrency.

---

## Design Philosophy

Squidbot chooses:

- Durable state over ephemeral context
- Inspectable memory over opaque recall
- Guardrails over surprises
- Automation that survives restarts

If you want an assistant that behaves like infrastructure instead of a toy, Squidbot is built for you.
