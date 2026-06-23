# stalker
Track every AI agent interaction, forever.

## Install

Install the `stalker` command with Go:

```bash
go install github.com/mohammad-safakhou/stalker/cmd/stalker@latest
```

For local development from this checkout:

```bash
make install
```

Make sure Go's binary directory is on your `PATH`. By default this is
`$(go env GOPATH)/bin`.

## Run

```bash
stalker
```

Defaults:

- Proxy and dashboard: `http://127.0.0.1:18080`
- Dashboard: `http://127.0.0.1:18080/ui/`
- Data directory:
  - macOS: `~/Library/Application Support/stalker/`
  - Linux/Unix: `${XDG_DATA_HOME:-~/.local/share}/stalker/`
  - Windows: `%LOCALAPPDATA%\stalker\`

For local development without installing:

```bash
make run
```

Configure Codex with:

```toml
openai_base_url = "http://127.0.0.1:18080/v1"
```

Do not set `chatgpt_base_url` yet. The proxy handles Codex ChatGPT-subscription
routing from the OpenAI-shaped traffic by detecting `ChatGPT-Account-ID` or the
OAuth JWT account claim.

## Storage

Stalker stores request metadata in SQLite with WAL mode enabled and writes large
request/response bodies under the data directory's `bodies/` folder. This keeps
browsing and searching fast while allowing large streaming responses to be
captured.

Environment variables:

- `STALKER_ADDR`: listen address, default `127.0.0.1:18080`
- `STALKER_DATA_DIR`: storage directory, default is the OS app data location
- `OPENAI_BASE_URL`: upstream OpenAI API base, default `https://api.openai.com/v1`
- `CHATGPT_BACKEND_URL`: upstream ChatGPT backend, default `https://chatgpt.com/backend-api`
- `CHATGPT_CODEX_URL`: upstream Codex backend, default `https://chatgpt.com/backend-api/codex`

## Setup

Run setup after installing:

```bash
stalker install
```

The setup command creates the data directory and can optionally configure a
service. For Codex, it writes this top-level setting to
`~/.codex/config.toml`:

```toml
openai_base_url = "http://127.0.0.1:18080/v1"
```

If you previously ran Stalker from a checkout and have data in `.stalker/`, move
it into the app data directory with:

```bash
stalker install --migrate --service codex
```

If an app-data database already exists, Stalker backs that directory up before
moving the old `.stalker/` directory.

Upgrade to the latest version with:

```bash
stalker upgrade
```
