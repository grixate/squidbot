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

- Dynamic provider model routing
- OpenClaw catalog parity support
- Multiple provider and channel profiles
- Graceful fallback behavior

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

1. Incoming message maps to a session actor
2. Actor loads bounded history from BoltDB
3. Prompt assembles memory, skills, and context
4. Tool/model loop runs under configured budgets
5. Events and usage are persisted
6. Memory files are updated and re-indexed

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

## Roadmap

Squidbot is built around a reliability-first core. Upcoming work focuses on making power features easier to operate at scale.

### ✅ Near-term

- **Configuration UI**
  - A friendly interface for workspace setup, providers/channels, budgets, memory, and skills
  - Validate configs before they go live (less "why is nothing working" time)

- **Mission Control**
  - Central dashboard for **tasks, resources, and analytics**
  - Track job runs, budgets, tool usage, latency, failures, and success rates
  - Clear "what happened, when, and why" views across sessions and automations

- **Federated Multi-Agent Management**
  - Visual control plane for subagents and federated nodes
  - Agent lifecycle, budgets, and permissions management
  - Peer health status, delegation history, and idempotent execution tracking

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
