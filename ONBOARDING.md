# Onboarding

Provider setup is mandatory before runtime commands (`agent`, `gateway`, `cron run`).

## Interactive Setup

```bash
./squidbot onboard
```

The CLI will ask you to:
- choose a provider
- enter required credentials
- set API base/model defaults
- optionally configure Telegram (`enabled`, `token`, allow list)
- optionally verify Gemini CLI connectivity for Gemini

## Non-Interactive Setup

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

- `--telegram-enabled` (requires `--telegram-token` when enabled)
- `--telegram-token <bot_token>`
- `--telegram-allow-from <id_or_username>` (repeatable, comma-separated supported)

## Verify Setup

```bash
./squidbot status
./squidbot doctor
```

If provider setup is incomplete, runtime commands will fail with:

```text
provider setup incomplete ... Run `squidbot onboard`
```
