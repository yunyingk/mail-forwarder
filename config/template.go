package config

const StarterConfig = `# mail-forwarder starter config
# Fill in real IMAP credentials and webhook URL before setting dry_run to false.

processing_mode: checkpoint_from_now

state:
  path: ./mail-forwarder-state.json

retry:
  backoff:
    - 5m
    - 30m
    - 2h
    - 6h
    - 24h

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
      secret: ""
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

admin:
  enabled: false
  listen: 127.0.0.1:6245

dry_run: true
`
