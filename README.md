# mail-forwarder

`mail-forwarder` is a small IMAP-to-HTTP ingress service.

It connects to configured IMAP mailbox folders, listens with IMAP IDLE, converts
unread mail into JSON, and posts each message to the webhook configured for that
mailbox source.

It does not run business rules, parse HTML into text, or route messages by
sender or keyword. Downstream HTTP agents should do that work.

## Features

- Multiple IMAP mailbox sources
- One HTTP webhook per mailbox source
- IMAP IDLE long-lived listening
- Explicit mail processing modes
- Persistent state for checkpoint modes and webhook retry cooldown
- Success acknowledgement: mark mail as seen only after webhook returns 2xx
- Structured JSON logs
- Optional local read-only admin API
- Cross-platform Go binary and Docker image

## Install

Download a release binary for your platform, or run with Docker:

```bash
docker compose pull
docker compose up -d
```

Build locally:

```bash
go build -ldflags="-s -w -X main.version=$(git describe --tags --always)" -o mail-forwarder .
```

## Configuration

Generate a starter config:

```bash
mail-forwarder init-config
```

This writes `config.yaml` in the current directory. If the file already exists,
it refuses to overwrite it. Use `-output` to choose another path:

```bash
mail-forwarder init-config -output config.yaml
```

The starter config uses placeholder IMAP credentials and `dry_run: true`. Edit
the config before production use.

Run:

```bash
mail-forwarder -config config.yaml
```

If `config.yaml` is missing, the program exits and tells you to run
`mail-forwarder init-config`.

## Processing Modes

`processing_mode` is required. Choose one:

- `unread_queue`
- `new_unread_queue`
- `checkpoint_from_now`
- `checkpoint_from_unread`

See [docs/processing-modes.md](docs/processing-modes.md) for exact behavior,
state semantics, and retry cooldown rules.

Recommended setup: create a dedicated mailbox folder and use mail-provider
rules to move target messages into that folder. Configure `mailbox` to watch
that folder. mail-forwarder only searches the configured mailbox, not the whole
email account.

## Webhook API

The API contract is maintained as OpenAPI:

- Source: `api/openapi.yaml`
- Generated JSON: `admin/openapi.json`

The OpenAPI document includes both surfaces:

- `/mail-ingress`: the outbound webhook request that downstream agents should implement.
- Local admin endpoints: `/healthz`, `/sources`, `/config`, `/openapi`, and `/openapi.json`.

`/mail-ingress` is not served by mail-forwarder. It documents the request shape
mail-forwarder sends to each configured webhook URL.

## Admin API

The local admin API is disabled by default and does not occupy a port unless
enabled:

```yaml
admin:
  enabled: true
  listen: 127.0.0.1:6245
```

Use `0.0.0.0:6245` only when you intentionally want the API reachable outside
the local machine or container.

Tools that import OpenAPI by URL can use:

```text
http://127.0.0.1:6245/openapi.json
```

## Project Structure

```text
main.go              # entrypoint and graceful shutdown
config/              # YAML config loading and starter template
mailer/              # IMAP connection, IDLE listening, message extraction
state/               # JSON state file for checkpoints and retry cooldown
webhook/             # HTTP webhook payload, signing, delivery
api/openapi.yaml     # OpenAPI source
admin/openapi.json   # generated OpenAPI JSON served by admin API
config.example.yaml  # starter config template
```

## Release Artifacts

The release workflow builds:

| Platform | File |
| --- | --- |
| Linux amd64 | `mail-forwarder-linux-amd64` |
| macOS amd64 | `mail-forwarder-darwin-amd64` |
| macOS arm64 | `mail-forwarder-darwin-arm64` |
| Windows amd64 | `mail-forwarder-windows-amd64.exe` |
