# MNI-5: Codex log and metric exposure investigation

Date: 2026-06-04

## Goal

Find practical ways for `stalker` to collect logs and metrics from Codex CLI and the Codex desktop app.

## Summary

Codex exposes useful telemetry through four layers:

1. OpenTelemetry export from Codex itself.
2. Lifecycle hooks that can mirror turn, prompt, tool, approval, and stop events.
3. `codex exec --json` for non-interactive automation streams.
4. Local state under `CODEX_HOME` plus enterprise Analytics and Compliance APIs.

The best initial integration for `stalker` is:

1. Configure Codex OpenTelemetry export for aggregate events and timings.
2. Add trusted user-level hooks for prompt/tool/approval/stop event ingestion.
3. Use `codex exec --json` only for workflows launched by `stalker`.
4. Treat local files and SQLite databases under `CODEX_HOME` as fallback/debug inputs, not the primary product integration.

## Documented Codex surfaces

### OpenTelemetry

Codex has a documented `[otel]` config section. It is disabled by default and can export structured log events over OTLP HTTP or gRPC.

Documented coverage includes:

- Conversation start events with model, reasoning settings, sandbox policy, and approval policy.
- API request attempts, status, success/failure, duration, and error details.
- SSE and WebSocket event metadata.
- User prompt length; prompt content is redacted unless `log_user_prompt = true`.
- Tool decisions, including approval/denial and whether the decision came from config or the user.
- Tool results, including duration, success, and an output snippet.
- Token counts on completed response events.

Example config:

```toml
[otel]
environment = "dev"
log_user_prompt = false
exporter = { otlp-http = {
  endpoint = "https://otel.example.com/v1/logs",
  protocol = "binary",
  headers = { "x-otlp-api-key" = "${OTLP_TOKEN}" }
}}
```

Important constraints:

- `otel` cannot be set from project-local `.codex/config.toml`; Codex ignores telemetry keys there.
- Configure telemetry at user level in `~/.codex/config.toml` or through managed/admin configuration.
- Prompt content should stay redacted by default. Enabling `log_user_prompt = true` materially changes privacy exposure.

### Lifecycle hooks

Codex hooks can run local commands inside the agent loop. The manual explicitly lists custom logging/analytics as a hook use case.

Relevant hook events:

- `SessionStart`
- `UserPromptSubmit`
- `PreToolUse`
- `PermissionRequest`
- `PostToolUse`
- `PreCompact`
- `PostCompact`
- `SubagentStart`
- `SubagentStop`
- `Stop`

For `stalker`, hooks are the most controllable way to collect exact operational events from local CLI/app sessions without scraping private local databases. Hooks can send sanitized JSON to a local daemon, append to a local queue file, or POST to an internal collector.

Important constraints:

- Hooks are enabled by default, but non-managed hooks must be reviewed and trusted.
- User-level hooks live in `~/.codex/hooks.json` or `~/.codex/config.toml`.
- Project-local hooks only load for trusted projects.
- Project-local config cannot define telemetry commands such as `notify` or `otel`, but project-local hooks can run when trusted.
- Managed hooks can be enforced through enterprise managed configuration.

### `codex exec --json`

For non-interactive Codex runs, `codex exec --json` writes JSONL events to stdout. Documented event types include:

- `thread.started`
- `turn.started`
- `turn.completed`
- `turn.failed`
- `item.*`
- `error`

Documented item types include agent messages, reasoning, command executions, file changes, MCP tool calls, web searches, and plan updates.

This is the cleanest option when `stalker` launches Codex itself. It is less useful for observing arbitrary human-started desktop or TUI sessions because it only covers the current `codex exec` process.

### Local Codex state

Codex stores local state under `CODEX_HOME`, defaulting to `~/.codex`. The manual documents common files such as:

- `config.toml`
- `auth.json` or OS credential storage
- `history.jsonl` when history persistence is enabled
- logs and caches

On this machine, `CODEX_HOME` is the default `~/.codex`. Observed without reading prompt or credential contents:

- CLI path: `/Users/safa.safakhou/.asdf/installs/nodejs/20.11.1/bin/codex`
- CLI version: `codex-cli 0.137.0`
- Local log SQLite: `~/.codex/logs_2.sqlite`
- Local state SQLite: `~/.codex/state_5.sqlite`
- Session rollout files: `~/.codex/sessions/YYYY/MM/DD/rollout-*.jsonl`
- Login troubleshooting log: `~/.codex/log/codex-login.log`
- History/index files: `~/.codex/history.jsonl`, `~/.codex/session_index.jsonl`

Observed SQLite schemas:

- `logs_2.sqlite.logs`: `ts`, `ts_nanos`, `level`, `target`, `feedback_log_body`, `module_path`, `file`, `line`, `thread_id`, `process_uuid`, `estimated_bytes`.
- `state_5.sqlite.threads`: thread ID, rollout path, timestamps, source, model provider, cwd, title, sandbox policy, approval mode, tokens used, git metadata, CLI version, first user message, model, reasoning effort, and preview.

Use these files carefully:

- They are local implementation details, not a stable ingestion API.
- They may contain prompt text, command output, file paths, repository names, tokens, or sensitive business data.
- `auth.json` must never be ingested.
- Session JSONL files and SQLite tables are useful for local debugging, backfill, or user-authorized import, but should not be tailed silently by default.

### Plaintext diagnostic logs

Codex supports an explicit `log_dir` setting. Setting `log_dir` enables the opt-in plaintext TUI log `codex-tui.log`.

Example:

```bash
RUST_LOG=debug codex -c log_dir=./.codex-log
tail -F ./.codex-log/codex-tui.log
```

For non-interactive mode, Codex prints messages inline instead of writing a separate TUI log.

### Enterprise Analytics and Compliance APIs

For ChatGPT Business/Enterprise workspaces, Codex governance exposes:

- Analytics Dashboard for usage/adoption and product-surface views.
- Analytics API for daily or weekly workspace and per-user usage metrics.
- Compliance API for detailed activity logs and metadata.

Analytics API coverage includes daily/weekly buckets, per-client breakdowns, Code Review throughput, threads, turns, credits, text input tokens, cached input tokens, and output tokens.

Compliance API coverage includes prompt text, responses, workspace/user/timestamp/model identifiers, token usage, and request metadata. Documented retention for ChatGPT-authenticated Codex Compliance API audit logs is up to 30 days. API-key-authenticated Codex usage follows API organization settings and is not included in ChatGPT Compliance API exports.

These APIs are the right source for org-level reporting and SIEM/eDiscovery integrations, but they require workspace admin setup and the right scopes.

## Recommended `stalker` architecture

### MVP

Build a local collector with two ingestion modes:

1. OTLP receiver for Codex `[otel]` events.
2. Hook receiver for events that OTel does not expose with enough context.

Recommended flow:

```text
Codex CLI / desktop app
  -> [otel] OTLP HTTP/gRPC exporter
  -> stalker local collector
  -> local queue / database
  -> optional remote ingest

Codex hooks
  -> sanitized JSON event
  -> stalker local collector
  -> same queue/database
```

Recommended default privacy posture:

- Do not enable `log_user_prompt` by default.
- Store prompt length/hash and explicit opt-in prompt text separately.
- Redact env vars and command outputs before upload.
- Never ingest `auth.json`.
- Make local session import a user-initiated action.

### Hook implementation shape

User-level `~/.codex/hooks.json` can call a stable `stalker` binary:

```json
{
  "hooks": {
    "UserPromptSubmit": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "stalker codex-hook UserPromptSubmit"
          }
        ]
      }
    ],
    "PostToolUse": [
      {
        "matcher": "*",
        "hooks": [
          {
            "type": "command",
            "command": "stalker codex-hook PostToolUse"
          }
        ]
      }
    ],
    "PermissionRequest": [
      {
        "matcher": "*",
        "hooks": [
          {
            "type": "command",
            "command": "stalker codex-hook PermissionRequest"
          }
        ]
      }
    ],
    "Stop": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "stalker codex-hook Stop"
          }
        ]
      }
    ]
  }
}
```

The actual hook input contract should be validated against a live hook run before implementation is finalized. The manual documents event names and matcher behavior, but the collector should tolerate missing/new fields.

### Data model

Use a normalized event model:

- `event_id`
- `source`: `codex_cli`, `codex_desktop`, `codex_exec`, `codex_enterprise`
- `source_version`
- `thread_id`
- `turn_id` when available
- `event_type`
- `timestamp`
- `cwd`
- `git_branch`
- `git_sha`
- `model`
- `approval_policy`
- `sandbox_policy`
- `duration_ms`
- `success`
- `token_usage`
- `privacy_level`: `metadata`, `redacted`, `content`
- `payload_json`

## Open questions

- Exact hook stdin payload shape needs a live hook fixture.
- Whether desktop app and CLI emit identical OTel event coverage should be verified with the same `~/.codex/config.toml` on a desktop run.
- Enterprise Analytics/Compliance API access depends on workspace plan, admin permissions, and scoped API keys.
- Local SQLite schemas are versioned implementation details and may change across Codex releases.

## Next steps

1. Create a small `stalker codex-hook` command that records raw hook payload fixtures locally with secrets redacted.
2. Configure Codex `[otel]` to a local OTLP receiver and capture sample events from CLI and desktop sessions.
3. Compare OTel coverage against hook coverage and decide which fields need hooks.
4. Add a user-facing consent model for prompt text, command output snippets, and session import.
5. Only after fixtures are captured, define the stable `stalker` event schema and persistence layer.

## Sources

- Official Codex manual fetched on 2026-06-04 from `https://developers.openai.com/codex/codex-manual.md`.
- Manual sections used: Config and state locations, Observability and telemetry, Log directory, Hooks, Non-interactive mode, Environment variables, Governance.
- Local observation on 2026-06-04: `codex-cli 0.137.0`, `~/.codex` directory structure, and SQLite schemas only. Credential and prompt/session contents were not read.
