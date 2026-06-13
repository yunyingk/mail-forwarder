# mail-forwarder

[简体中文](README.md) | [日本語](README.ja.md) | [한국어](README.ko.md)

`mail-forwarder` is a small IMAP-to-HTTP ingress service for turning mailbox
messages into webhook calls.

It connects to configured IMAP mailbox folders, listens with IMAP IDLE, converts
unread mail into JSON, and posts each message to the webhook configured for that
mailbox source.

It does not run business rules, parse HTML into text, or route messages by
sender or keyword. Downstream HTTP agents should do that work.

## Why

Many systems can receive HTTP webhooks but cannot subscribe to email directly.
`mail-forwarder` keeps that boundary simple: this process only owns IMAP
connection, message extraction, delivery acknowledgement, and retry cooldown.

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

## Quick Start

Generate a starter config:

```bash
mail-forwarder init-config
```

Edit `config.yaml`, then run:

```bash
mail-forwarder -config config.yaml
```

The starter config uses placeholder IMAP credentials and `dry_run: true`. Replace
the IMAP account and webhook URL before production use.

## Install

Download a release binary from GitHub Releases, or run with Docker:

```bash
docker compose pull
docker compose up -d
```

Published images:

```text
ghcr.io/yunyingk/mail-forwarder:v0.2.2
crpi-yg3r8e9fvlik3jk0.cn-hangzhou.personal.cr.aliyuncs.com/yingqing/mail-forwarder:v0.2.2
```

Build locally:

```bash
go build -ldflags="-s -w -X main.version=$(git describe --tags --always)" -o mail-forwarder .
```

## Configuration

`mail-forwarder init-config` writes `config.yaml` in the current directory. If
the file already exists, it refuses to overwrite it. Use `-output` to choose
another path:

```bash
mail-forwarder init-config -output /etc/mail-forwarder/config.yaml
```

If `config.yaml` is missing, the program exits and tells you to run
`mail-forwarder init-config`.

Minimal shape:

```yaml
processing_mode: checkpoint_from_now
state:
  path: ./mail-forwarder-state.json
imap:
  - name: example-inbox
    host: imap.example.com
    port: 993
    secure: true
    user: user@example.com
    pass: ${IMAP_PASS}
    mailbox: INBOX
    webhook:
      url: http://127.0.0.1:3000/mail-ingress
dry_run: true
```

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

## Delivery Semantics

- A message is marked as seen only after the webhook returns HTTP 2xx.
- Webhook failures keep the message unread.
- Failed messages enter local retry cooldown before the next attempt.
- Checkpoint modes suppress successful UID re-delivery.
- `unread_queue` modes intentionally allow manual unread changes to send again.

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

## Documentation

- [Processing modes](docs/processing-modes.md)
- [OpenAPI YAML](api/openapi.yaml)

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

Docker images are published by the release workflow to GHCR and Aliyun ACR.
