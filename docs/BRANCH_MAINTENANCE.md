# Branch Maintenance Guide

This repository intentionally keeps two long-lived branches:

- `main`: core Go and CLI runtime only.
- `squidbot-ui`: visual onboarding + management server + management web UI.

## Merge Direction

- Merge `main` into `squidbot-ui` regularly.
- Do not merge `squidbot-ui` back into `main`.

## Common Conflict Zones

When merging `main` into `squidbot-ui`, expect conflicts in:

- `cmd/squidbot/main.go`
- `internal/config/config.go`
- UI and management trees:
  - `internal/management/**`
  - `web/management-ui/**`

## Conflict Resolution Rule

On `squidbot-ui`, keep UI/management behavior and files where conflicts occur, while still taking core runtime updates from `main`.
