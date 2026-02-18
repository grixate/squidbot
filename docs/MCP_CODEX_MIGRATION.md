# MCP + OpenAI Codex Migration Guide

This guide covers migrating existing Squidbot setups to the new MCP and OpenAI Codex OAuth features.

## 1) Baseline

Before changing config:

```bash
squidbot status
squidbot doctor
```

## 2) Enable OpenAI Codex OAuth (optional)

1. Set feature flag:

```json
{
  "features": {
    "codexOAuth": true
  },
  "providers": {
    "active": "openai-codex",
    "registry": {
      "openai-codex": {
        "model": "openai-codex/gpt-5.1-codex"
      }
    }
  }
}
```

2. Complete login:

```bash
squidbot provider login openai-codex
```

3. Verify:

```bash
squidbot provider status openai-codex
squidbot status
```

Notes:
- OAuth token is stored in `~/.squidbot/oauth/openai-codex.json`.
- Token material is never written to `config.json`.

## 3) Enable MCP tools (optional)

1. Set feature flag and MCP config:

```json
{
  "features": {
    "mcp": true
  },
  "tools": {
    "mcp": {
      "enabled": true,
      "connectTimeoutSec": 20,
      "servers": {
        "filesystem": {
          "enabled": true,
          "command": "npx",
          "args": ["-y", "@modelcontextprotocol/server-filesystem", "/path/to/workspace"]
        },
        "remote": {
          "enabled": true,
          "url": "https://mcp.example.com/rpc"
        }
      }
    }
  }
}
```

2. Validate:

```bash
squidbot doctor
squidbot status
```

Notes:
- Enabled MCP servers must configure exactly one transport (`command` or `url`).
- Connection failures are non-fatal per-server; healthy servers still register tools.

## 4) Rollback

Disable the feature flags to restore old behavior:

```json
{
  "features": {
    "codexOAuth": false,
    "mcp": false
  },
  "tools": {
    "mcp": {
      "enabled": false
    }
  }
}
```

Optional Codex logout:

```bash
squidbot provider logout openai-codex
```
