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
- LAN sync API for native apps: `http://<mac-lan-ip>:18081/api/v1/sync/snapshot`
- Data directory:
  - macOS: `~/Library/Application Support/stalker/`
  - Linux/Unix: `${XDG_DATA_HOME:-~/.local/share}/stalker/`
  - Windows: `%LOCALAPPDATA%\stalker\`

For local development without installing:

```bash
make run
```

Restarting Stalker interrupts any active client traffic routed through the
proxy. If you are using Codex through Stalker, stop work or switch Codex away
from the proxy before restarting the daemon.

Configure Codex with:

```toml
openai_base_url = "http://127.0.0.1:18080/v1"
```

Do not set `chatgpt_base_url` yet. The proxy handles Codex ChatGPT-subscription
routing from the OpenAI-shaped traffic by detecting `ChatGPT-Account-ID` or the
OAuth JWT account claim.

## Storage

Stalker stores request metadata in SQLite with WAL mode enabled and writes large
request/response bytes through an in-memory token pipeline. Raw payloads are not
retained by default. The database stores exchange metadata, per-exchange token
runs, and global per-token aggregate counts.

## Native sync

Stalker exposes aggregate-only sync APIs for native Apple clients:

- `GET /api/v1/sync/snapshot`: device info, token totals, live stats, hourly buckets, daily buckets, and top token aggregates
- `GET /api/v1/sync/stream`: near-live Server-Sent Events using the same privacy-safe snapshot shape

These endpoints are available on the main localhost server and on a separate
sync-only listener. The default sync listener is `0.0.0.0:18081`, advertised on
Bonjour as `_stalker._tcp`, and does not serve the proxy.

The sync payload intentionally excludes request/response bodies, previews,
headers, and body paths.

Native Apple source lives under `apple/StalkerApple/`:

- `StalkerMac`: SwiftUI macOS menu bar app
- `StalkerPhone`: SwiftUI iPhone dashboard source
- `StalkerWatch`: SwiftUI Apple Watch dashboard source
- `StalkerWidgets`: WidgetKit complication source
- `StalkerShared`: shared models, localhost client, CloudKit writer, WatchConnectivity bridge, and dashboard views

Local checks:

```bash
cd apple/StalkerApple
swift test
swift build --target StalkerMac
```

Environment variables:

- `STALKER_ADDR`: listen address, default `127.0.0.1:18080`
- `STALKER_SYNC_ADDR`: sync-only listen address, default `0.0.0.0:18081`; set empty to disable
- `STALKER_DATA_DIR`: storage directory, default is the OS app data location
- `STALKER_BONJOUR`: Bonjour discovery, default enabled; set `0` to disable
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

Remove raw payload data retained by older versions and shrink the database with:

```bash
stalker compact --yes
```
