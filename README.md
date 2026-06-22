# stalker
Track every AI agent interaction, forever.

## Run

```bash
go run ./cmd
```

Defaults:

- Proxy and dashboard: `http://127.0.0.1:18080`
- Dashboard: `http://127.0.0.1:18080/ui/`
- Data directory: `.stalker/`

Configure Codex with:

```toml
openai_base_url = "http://127.0.0.1:18080/v1"
```

Do not set `chatgpt_base_url` yet. The proxy handles Codex ChatGPT-subscription
routing from the OpenAI-shaped traffic by detecting `ChatGPT-Account-ID` or the
OAuth JWT account claim.

## Storage

Stalker stores request metadata in SQLite with WAL mode enabled and writes large
request/response bodies to files under `.stalker/bodies/`. This keeps browsing
and searching fast while allowing large streaming responses to be captured.

Environment variables:

- `STALKER_ADDR`: listen address, default `127.0.0.1:18080`
- `STALKER_DATA_DIR`: storage directory, default `.stalker`
- `OPENAI_BASE_URL`: upstream OpenAI API base, default `https://api.openai.com/v1`
- `CHATGPT_BACKEND_URL`: upstream ChatGPT backend, default `https://chatgpt.com/backend-api`
- `CHATGPT_CODEX_URL`: upstream Codex backend, default `https://chatgpt.com/backend-api/codex`
