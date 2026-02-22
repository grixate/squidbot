# Default-On Proposal: MCP + OpenAI Codex OAuth

This document defines the phase-4 readiness criteria for making these features default-on.

## Scope

- `features.codexOAuth`
- `features.mcp`
- `tools.mcp.enabled`

## Required Validation Window

Run a minimum **14-day** validation window in representative environments.

## Readiness Gates

1. Stability
- `go test ./...` stays green throughout rollout updates.
- No unresolved P1/P2 bugs linked to MCP transport or Codex OAuth token lifecycle.

2. Runtime Health
- `squidbot doctor` passes on pilot deployments.
- MCP-enabled deployments show at least one healthy MCP server registration where configured.

3. Auth Reliability
- Codex login/status/logout flow succeeds in CI/integration environments.
- Token refresh path validated with expiring access tokens.

4. Backward Compatibility
- Non-MCP and non-Codex setups show no behavior regression.
- Existing provider flows (OpenAI-compatible and Anthropic) remain unchanged.

## Suggested Rollout Steps

1. Expand pilot usage while flags remain explicit.
2. Gather incident/bug data and classify by severity.
3. If all gates pass, switch defaults in `config.Default()`:
- `features.codexOAuth=true`
- `features.mcp=true`
- `tools.mcp.enabled=true`
4. Keep documented rollback path for one release cycle.

## Rollback Criteria

Immediately revert defaults if any of the following occurs:
- repeated OAuth login/refresh failures in stable environments
- MCP transport regressions causing agent startup instability
- tool-loop regressions in parent/subagent runtimes
