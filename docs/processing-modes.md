# Processing Modes

`mail-forwarder` watches one configured IMAP mailbox folder per source. It does
not scan the whole email account. For production use, create a dedicated mailbox
folder and use mail-provider rules to move target messages into that folder.

`processing_mode` is required because the service changes mail `Seen` state
after successful delivery.

## Modes

### `unread_queue`

Use the mailbox's unread state as the queue.

- Existing unread mail is delivered.
- New unread mail is delivered.
- If a delivered mail is manually changed back to unread, it is delivered again.
- No checkpoint is used to suppress re-delivery.

Use this for a dedicated queue-like mailbox folder.

### `new_unread_queue`

Use the service startup UID as a baseline.

- Existing unread mail before service startup is skipped.
- New unread mail after startup is delivered.
- If a delivered mail after startup is manually changed back to unread, it is delivered again.
- Restarting the service creates a new startup baseline.

Use this when you do not want to touch old unread mail and do not need restart
catch-up semantics.

### `checkpoint_from_now`

Use a persistent UID checkpoint and initialize it from the current mailbox end.

- First run skips existing unread mail.
- New unread mail after initialization is delivered.
- Delivered UID values are not delivered again after success.
- Restarting the service continues from the stored checkpoint.

Use this as the safest default for personal or existing mailboxes.

### `checkpoint_from_unread`

Use a persistent UID checkpoint and initialize it from the unread backlog.

- First run delivers existing unread mail.
- New unread mail is delivered.
- Delivered UID values are not delivered again after success.
- Restarting the service continues from the stored checkpoint.

Use this for a dedicated queue-like mailbox folder when you also want durable
checkpoint behavior.

## State

State is stored in a JSON file configured by:

```yaml
state:
  path: ./mail-forwarder-state.json
```

The state file stores:

- per-source checkpoint progress for checkpoint modes
- per-message retry cooldown after webhook failures

Message identity uses:

```text
source.name + mailbox + uid_validity + uid
```

IMAP UID values are scoped to one mailbox and one UIDVALIDITY period. They are
not globally unique across mailboxes or providers.

## Retry Cooldown

Webhook failures keep mail unread. To avoid repeatedly hitting a broken
downstream service, failed messages are placed in a local cooldown:

```yaml
retry:
  backoff:
    - 5m
    - 30m
    - 2h
    - 6h
    - 24h
```

On each failure, `next_attempt_at` is moved forward using the next backoff
duration. After the last configured duration, the final duration is reused.

If a mailbox update or service restart sees the same unread message before
`next_attempt_at`, the message is skipped without fetching the body or calling
the webhook.

Successful delivery clears the retry state.
