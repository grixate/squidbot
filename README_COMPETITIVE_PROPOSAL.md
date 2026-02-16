# Squidbot Competitive README Proposal

Snapshot date: 2026-02-15.

## Goal

Produce a README direction that competes with top OpenClaw ecosystem projects by being:

- Clear on positioning in the first screen.
- Credible with proof-backed claims.
- Easy to try in under 2 minutes.
- Strong on reliability and safety messaging.

## Competitive Baseline (OpenClaw Ecosystem)

Reference projects and current traction snapshot:

| Project | Stars | README style pattern worth copying |
| --- | ---: | --- |
| `openclaw/openclaw` | 196,735 | Strong first-screen pitch, badges, fast links, onboarding path, clear highlights |
| `openclaw/clawhub` | 2,032 | Tight value proposition + task-oriented capability bullets |
| `openclaw/skills` | 1,017 | Minimal; weak positioning (opportunity for Squidbot to win on clarity) |
| `openclaw/lobster` | 466 | Concrete examples that prove behavior, not generic claims |

## What Makes Squidbot Strong (Proposal Pillars)

## 1) Reliability-first runtime architecture

Why it matters: users trust assistants that do not lose context or crash under concurrency.

Evidence in Squidbot:

- Actor system with bounded mailboxes and idle actor eviction.
- Explicit mailbox overflow protection (`ErrMailboxFull`) instead of silent degradation.
- Per-session checkpoint load/save.

Product message:

- "Predictable session isolation with backpressure and recoverable state."

## 2) Durable-by-default state model

Why it matters: many assistants are stateless wrappers; Squidbot is operational software.

Evidence in Squidbot:

- BoltDB buckets for turns, tool events, jobs, job runs, checkpoints, mission data, subagent/federation events, and budget counters.
- Single writer queue pattern for controlled writes.

Product message:

- "Durable runtime ledger for every action: turns, tools, jobs, and safety events."

## 3) Human-editable memory + indexed retrieval

Why it matters: users want memory they can inspect and edit, not black-box recall.

Evidence in Squidbot:

- `memory/MEMORY.md` + `memory/daily/*.md` as source of truth.
- SQLite index with chunk upsert/delete reconciliation and FTS retrieval.
- Hybrid scoring with optional semantic rerank; lexical fallback guaranteed.

Product message:

- "Editable memory files with fast retrieval and optional semantic reranking."

## 4) Practical multi-provider and OpenClaw catalog parity

Why it matters: provider lock-in is a frequent reason teams switch stacks.

Evidence in Squidbot:

- Dynamic provider support merges built-ins with OpenClaw provider catalog.
- Current parity catalog in this repo includes 15 provider profiles and 20 channel profiles.

Product message:

- "Built for provider portability, not single-vendor dependency."

## 5) Channel strategy that balances native quality with fallback compatibility

Why it matters: platform integrations fail in the real world; fallback keeps uptime high.

Evidence in Squidbot:

- Native adapters wired for Telegram, Slack, Discord, WebChat, and WhatsApp.
- Webhook/noop fallback behavior for other enabled channels.

Product message:

- "Native where needed, webhook fallback where practical."

## 6) Operational guardrails (a differentiator many READMEs skip)

Why it matters: "AI assistant" buyers now ask for operational controls first.

Evidence in Squidbot:

- `doctor` checks for provider, workspace contract, memory index, skills, and federation config health.
- Token safety subsystem: preflight reservations, hard limits, soft warnings, and accounting.
- Prometheus metrics endpoint with localhost and bearer-token controls.

Product message:

- "Runs like production software: health checks, budgets, and metrics built in."

## 7) Automation and distributed execution primitives

Why it matters: advanced users want scheduled and delegated work, not only chat.

Evidence in Squidbot:

- Cron service with deliver-back options.
- Heartbeat service with persisted run history.
- Subagent lifecycle controls.
- Federation HTTP client/server with node auth, idempotency keys, and peer health tracking.

Product message:

- "From chat to autonomous workflows with delegation-ready architecture."

## 8) Quality signal from tests and race checks

Why it matters: README trust increases when claims are backed by verification discipline.

Evidence in Squidbot:

- 40 Go test files.
- `go test ./...` and `go test -race ./...` pass.

Product message:

- "Built with test and race-validation discipline across core runtime components."

## Recommended README Positioning

Primary audience:

- Builders who value reliability and control over flashy demos.

Core positioning line:

- "Reliability-first personal AI assistant in Go, with durable state, controllable memory, and operations-grade guardrails."

Narrative order:

1. Positioning and trust signals (what Squidbot is, who it is for).
2. Differentiators (runtime reliability, memory, safety, automation).
3. 60-second quickstart.
4. Capability matrix (providers/channels/ops).
5. Architecture and persistence model.
6. CLI and onboarding references.
7. Testing and contribution path.

## Gaps In Current README (and Fixes)

- Gap: message says channel scope is narrow (Telegram + CLI) while runtime already supports more adapters.
  Fix: replace with explicit "native + fallback" channel model.

- Gap: strengths are listed but not framed as buyer-value outcomes.
  Fix: pair every feature bullet with outcome language ("why it matters").

- Gap: no competitive context.
  Fix: add "Why Squidbot" section with design tradeoffs (reliability over feature sprawl).

## Draft: Competitive README (Drop-in)

````markdown
# squidbot

Reliability-first personal AI assistant in Go, built for durable operation: actor-isolated sessions, persistent runtime state, editable memory, and production-style guardrails.

## Why Squidbot

Most assistant projects optimize for demos. Squidbot optimizes for long-running reliability:

- Predictable concurrency with per-session actor isolation and bounded mailboxes.
- Durable BoltDB runtime ledger for turns, jobs, checkpoints, runs, usage, and safety events.
- Human-editable markdown memory with SQLite indexing and hybrid retrieval.
- Operations-grade controls: doctor checks, token safety budgets, and metrics export.

If you want an assistant that behaves like dependable infrastructure, Squidbot is designed for that job.

## Highlights

- Actor runtime with mailbox backpressure + idle actor eviction
- BoltDB primary store for sessions, turns, cron, mission, federation, subagents, and budgets
- Markdown memory (`memory/MEMORY.md`, `memory/daily/*.md`) + SQLite FTS index
- Optional semantic reranking over lexical retrieval
- Cron + heartbeat services with persisted run records
- Multi-provider model routing with OpenClaw parity catalog support
- Native channel adapters: Telegram, Slack, Discord, WebChat, WhatsApp
- Webhook fallback path for additional channels
- Skills runtime with discovery, routing, policy filters, and progressive disclosure
- Token safety guardrails (limits, reservations, warnings, usage accounting)
- Federation primitives for delegated work across trusted nodes
- Prometheus-style metrics endpoint controls

## Quick Start

```bash
go build -o squidbot ./cmd/squidbot
./squidbot onboard
./squidbot agent -m "Hello"
./squidbot gateway
./squidbot doctor
```

## Local Isolated Dev Mode

Use clone-local state (instead of `~/.squidbot`):

```bash
./scripts/dev-squidbot.sh onboard
./scripts/dev-squidbot.sh gateway
./scripts/dev-squidbot.sh status
./scripts/dev-squidbot.sh where
```

## Architecture At A Glance

1. Inbound message maps to a session actor.
2. Actor loads bounded history from BoltDB.
3. Prompt assembles bootstrap docs + memory retrieval + activated skills.
4. Tool/model loop executes under configured limits.
5. Turns/events/usage are persisted.
6. Episodic memory is appended and index is reconciled.

## Persistence Model

| Layer | Location | Purpose |
| --- | --- | --- |
| Runtime DB | `~/.squidbot/data/squidbot.db` | Sessions, turns, tool events, jobs/runs, checkpoints, mission, federation, subagent, budgets, usage |
| Memory source | `<workspace>/memory/MEMORY.md`, `<workspace>/memory/daily/*.md` | Editable long-term + episodic memory |
| Memory index | `~/.squidbot/data/memory_index.db` | Chunk index, FTS retrieval, optional semantic rerank data |

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

## Key Commands

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
- `squidbot skills list`
- `squidbot skills show <skill_id>`
- `squidbot skills check [--strict]`
- `squidbot skills reload`
- `squidbot budget status`
- `squidbot doctor`

## Testing

```bash
go test ./...
go test -race ./...
```

## Design Principle

Reliability over feature sprawl:

- State is durable.
- Memory is inspectable.
- Operations are observable.
- Automation is controllable.
````

## Rollout Plan

1. Replace `README.md` with the drop-in draft above.
2. Add one architecture diagram screenshot/GIF under `docs/assets/`.
3. Add one end-to-end "cron -> message delivery" example.
4. Add one "token safety limit reached" example output.
5. Keep channel and provider claims synced with runtime capability checks.
