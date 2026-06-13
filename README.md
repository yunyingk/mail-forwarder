# mail-forwarder

`mail-forwarder` is a small IMAP-to-HTTP ingress service.

It connects to one or more IMAP mailboxes, listens for unread mail with IMAP
IDLE, converts each message into a JSON payload, and posts it to the webhook
configured for that mailbox.

It does not run business rules, parse HTML into text, or route messages by
sender or keyword. Downstream HTTP agents should do that work.

## Features

- Multiple IMAP mailbox sources
- One HTTP webhook per mailbox source
- IMAP IDLE long-lived listening
- Startup backlog processing for unread messages
- Success acknowledgement: mark mail as seen only after webhook returns 2xx
- Failure retry: keep mail unread when webhook fails, times out, or returns non-2xx
- Structured JSON logs
- Cross-platform Go binary: Linux / macOS / Windows
- Docker deployment

## Install

### Binary

Download a release binary for your platform.

### Docker

```bash
docker compose pull
docker compose up -d
```

### Build Locally

```bash
go build -ldflags="-s -w -X main.version=$(git describe --tags --always)" -o mail-forwarder .
```

## Configuration

```bash
cp config.example.yaml config.yaml
```

Example:

```yaml
imap:
  - name: hesi-mailbox
    host: imap.exmail.qq.com
    port: 993
    secure: true
    user: alert@example.com
    pass: your_imap_password
    mailbox: INBOX
    webhook:
      url: https://example.com/mail-ingress
      secret: your_webhook_secret
      timeout_sec: 10
      headers:
        X-Agent-Name: mail-forwarder
    payload:
      include_raw_rfc822: false
      attachments: disabled # disabled, metadata, inline_base64
    idle_fallback:
      allow: false
      interval_sec: 60
    timeouts:
      connection_sec: 15
      socket_sec: 300

dry_run: false
poll_on_start: true
```

Environment variables can be referenced in config values:

```yaml
pass: ${IMAP_PASS}
```

## Delivery Semantics

For every configured mailbox:

1. On startup, unread messages are processed when `poll_on_start: true`.
2. The service enters IMAP IDLE and waits for mailbox updates.
3. When unread mail is found, messages are processed by UID in order.
4. The webhook is called once per message.
5. If the webhook returns HTTP 2xx, the message is marked as seen.
6. If the webhook fails, times out, or returns non-2xx, the message remains unread.

This means disconnects and webhook failures are retried after reconnect or the
next mailbox update. If ten unread messages accumulate while the service is
offline, all ten are delivered after reconnect.

`dry_run: true` logs the webhook delivery that would happen and does not mark
messages as seen.

If `idle_fallback.allow: false`, mailboxes that do not support IMAP IDLE fail
and reconnect instead of falling back to polling. Set `idle_fallback.allow: true`
to let `go-imap` use periodic NOOP polling with `idle_fallback.interval_sec`.

## Webhook API

`mail-forwarder` sends an HTTP `POST` request to the configured webhook URL.

Headers:

```http
Content-Type: application/json
User-Agent: mail-forwarder
X-Mail-Forwarder-Timestamp: 1718265600
X-Mail-Forwarder-Signature: sha256=<hex-hmac>
```

`X-Mail-Forwarder-Timestamp` and `X-Mail-Forwarder-Signature` are sent only
when `webhook.secret` is configured.

Signature input:

```text
<unix_timestamp>.<raw_request_body>
```

Signature algorithm:

```text
HMAC-SHA256(secret, input)
```

### Request Body

```json
{
  "source": {
    "name": "hesi-mailbox",
    "mailbox": "INBOX"
  },
  "message": {
    "uid": 12345,
    "message_id": "<message-id@example.com>",
    "date": "2026-06-13T12:30:00+08:00",
    "subject": "Example subject",
    "from": "notice@example.com",
    "to": ["alert@example.com"],
    "cc": [],
    "reply_to": [],
    "headers": {
      "message-id": "<message-id@example.com>",
      "return-path": "<notice@example.com>",
      "authentication-results": "..."
    },
    "bodies": {
      "text": "plain text body",
      "html": "<html><body>html body</body></html>"
    },
    "attachments": [
      {
        "filename": "invoice.pdf",
        "content_type": "application/pdf",
        "content_id": "",
        "disposition": "attachment; filename=\"invoice.pdf\"",
        "size": 123456,
        "content_base64": "JVBERi0x..."
      }
    ],
    "raw": {
      "rfc822_base64": "RnJvbTog...",
      "size": 456789,
      "included": true
    }
  }
}
```

Notes:

- `bodies.text` contains the `text/plain` part when present.
- `bodies.html` contains the `text/html` part when present.
- HTML is forwarded as-is. This service does not sanitize or interpret it.
- `payload.include_raw_rfc822: true` includes the original RFC822 message as base64.
- `payload.attachments: metadata` includes attachment filename, content type, and size.
- `payload.attachments: inline_base64` also includes attachment bytes as base64.
- `payload.attachments: disabled` omits attachments.

## Run

```bash
./mail-forwarder -config config.yaml
```

Docker:

```bash
docker compose up -d
docker logs -f mail-forwarder
```

## Project Structure

```text
main.go              # entrypoint and graceful shutdown
config/              # YAML config loading
mailer/              # IMAP connection, IDLE listening, message extraction
webhook/             # HTTP webhook payload, signing, delivery
config.example.yaml  # config template
```

## Release Artifacts

The release workflow builds:

| Platform | File |
| --- | --- |
| Linux amd64 | `mail-forwarder-linux-amd64` |
| macOS amd64 | `mail-forwarder-darwin-amd64` |
| macOS arm64 | `mail-forwarder-darwin-arm64` |
| Windows amd64 | `mail-forwarder-windows-amd64.exe` |
