import dns from 'node:dns';
import crypto from 'node:crypto';
import process from 'node:process';
import axios from 'axios';
import { ImapFlow } from 'imapflow';
import { simpleParser } from 'mailparser';

dns.setDefaultResultOrder('ipv4first');

const config = {
  imapHost: requiredEnv('IMAP_HOST'),
  imapPort: numberEnv('IMAP_PORT', 993),
  imapSecure: boolEnv('IMAP_SECURE', true),
  imapUser: requiredEnv('IMAP_USER'),
  imapPass: requiredEnv('IMAP_PASS'),
  mailbox: env('MAILBOX', 'INBOX'),
  filterFrom: normalizeEmail(requiredEnv('FILTER_FROM')),
  filterSubjectKeyword: env('FILTER_SUBJECT_KEYWORD', ''),
  dryRun: boolEnv('DRY_RUN', false),
  markSeenOnDryRun: boolEnv('MARK_SEEN_ON_DRY_RUN', false),
  dingtalkTitle: env('DINGTALK_TITLE', '合思工作流失败提醒'),
  dingtalkWebhook: env('DINGTALK_WEBHOOK', ''),
  dingtalkSecret: env('DINGTALK_SECRET', ''),
  httpTimeoutMs: numberEnv('HTTP_TIMEOUT_MS', 10000),
  imapConnectionTimeoutMs: numberEnv('IMAP_CONNECTION_TIMEOUT_MS', 15000),
  imapSocketTimeoutMs: numberEnv('IMAP_SOCKET_TIMEOUT_MS', 300000),
  pollOnStart: boolEnv('POLL_ON_START', true),
  maxTextLength: numberEnv('MAX_TEXT_LENGTH', 3200)
};

let shuttingDown = false;
let processing = false;
let pendingScan = false;

process.on('SIGTERM', () => {
  shuttingDown = true;
  console.log('received SIGTERM, shutting down');
});

process.on('SIGINT', () => {
  shuttingDown = true;
  console.log('received SIGINT, shutting down');
});

main().catch((error) => fatal(error));

async function main() {
  if (!config.dryRun && !config.dingtalkWebhook) {
    throw new Error('missing required env: DINGTALK_WEBHOOK');
  }

  console.log(`starting mail-forwarder for ${config.imapUser}, mailbox=${config.mailbox}`);
  if (config.dryRun) {
    console.log('DRY_RUN=true: DingTalk will not be called');
  }

  const client = new ImapFlow({
    host: config.imapHost,
    port: config.imapPort,
    secure: config.imapSecure,
    auth: {
      user: config.imapUser,
      pass: config.imapPass
    },
    connectionTimeout: config.imapConnectionTimeoutMs,
    socketTimeout: config.imapSocketTimeoutMs,
    tls: {
      family: 4
    },
    logger: false
  });

  client.on('error', (error) => {
    fatal(error);
  });

  client.on('exists', (data) => {
    console.log(`mailbox exists changed: ${data.prevCount} -> ${data.count}`);
    scheduleScan(client);
  });

  await client.connect();
  console.log('imap connected');

  const lock = await client.getMailboxLock(config.mailbox);
  try {
    await client.mailboxOpen(config.mailbox);
    console.log(`mailbox opened: ${config.mailbox}`);

    if (config.pollOnStart) {
      await processUnread(client);
    }

    while (!shuttingDown) {
      try {
        await client.idle();
      } catch (error) {
        fatal(error);
      }
    }
  } finally {
    lock.release();
    await client.logout().catch(() => {});
  }
}

function scheduleScan(client) {
  if (pendingScan || shuttingDown) {
    return;
  }

  pendingScan = true;
  setTimeout(() => {
    pendingScan = false;
    processUnread(client).catch((error) => fatal(error));
  }, 500);
}

async function processUnread(client) {
  if (processing) {
    return;
  }

  processing = true;
  try {
    const uids = await client.search({ seen: false }, { uid: true });
    if (!uids.length) {
      return;
    }

    console.log(`found ${uids.length} unread message(s)`);

    for (const uid of uids) {
      if (shuttingDown) {
        return;
      }

      await processOne(client, uid);
    }
  } finally {
    processing = false;
  }
}

async function processOne(client, uid) {
  const summary = await client.fetchOne(uid, {
    envelope: true,
    flags: true
  }, { uid: true });

  if (!summary) {
    return;
  }

  const from = firstAddress(summary.envelope?.from);
  if (from !== config.filterFrom) {
    return;
  }

  const subject = summary.envelope?.subject || '';
  if (config.filterSubjectKeyword && !subject.includes(config.filterSubjectKeyword)) {
    return;
  }

  const message = await client.fetchOne(uid, {
    envelope: true,
    source: true
  }, { uid: true });

  if (!message) {
    return;
  }

  const parsed = await simpleParser(message.source);
  const text = parsed.text || htmlToTextFallback(parsed.html) || '';
  const payload = buildDingTalkPayload({
    uid,
    messageId: parsed.messageId || summary.envelope?.messageId || message.envelope?.messageId || '',
    from,
    subject: parsed.subject || summary.envelope?.subject || message.envelope?.subject || '',
    date: parsed.date || summary.envelope?.date || message.envelope?.date || null,
    text
  });

  if (config.dryRun) {
    console.log('dry-run payload:', JSON.stringify(payload.meta, null, 2));
    if (!config.markSeenOnDryRun) {
      console.log(`dry-run skipped mark seen: uid=${uid}`);
      return;
    }
  }

  try {
    if (!config.dryRun) {
      await postToDingTalk(payload);
    }
    await client.messageFlagsAdd(uid, ['\\Seen'], { uid: true });
    console.log(`forwarded and marked seen: uid=${uid}, subject=${JSON.stringify(payload.meta.subject)}`);
  } catch (error) {
    await client.messageFlagsRemove(uid, ['\\Seen'], { uid: true }).catch(() => {});
    throw error;
  }
}

function buildDingTalkPayload(mail) {
  const date = mail.date instanceof Date ? mail.date.toISOString() : '';
  const body = trimText(mail.text, config.maxTextLength);
  const content = [
    `### ${config.dingtalkTitle}`,
    '',
    body || '(无正文)'
  ].join('\n');

  return {
    dingtalk: {
      msgtype: 'markdown',
      markdown: {
        title: config.dingtalkTitle,
        text: content
      }
    },
    meta: {
      uid: mail.uid,
      messageId: mail.messageId,
      from: mail.from,
      subject: mail.subject,
      date,
      text: body
    }
  };
}

async function postToDingTalk(payload) {
  const url = signedDingTalkUrl(config.dingtalkWebhook, config.dingtalkSecret);
  await axios.post(url, payload.dingtalk, {
    timeout: config.httpTimeoutMs,
    headers: {
      'Content-Type': 'application/json'
    },
    validateStatus: (status) => status >= 200 && status < 300
  });
}

function signedDingTalkUrl(webhook, secret) {
  if (!secret) {
    return webhook;
  }

  const timestamp = Date.now();
  const sign = crypto
    .createHmac('sha256', secret)
    .update(`${timestamp}\n${secret}`)
    .digest('base64');

  const url = new URL(webhook);
  url.searchParams.set('timestamp', String(timestamp));
  url.searchParams.set('sign', sign);
  return url.toString();
}

function firstAddress(addresses = []) {
  const first = addresses[0];
  return normalizeEmail(first?.address || '');
}

function normalizeEmail(value) {
  return String(value).trim().toLowerCase();
}

function trimText(value, maxLength) {
  const text = String(value || '').replace(/\r\n/g, '\n').trim();
  if (text.length <= maxLength) {
    return text;
  }

  return `${text.slice(0, maxLength)}\n...(truncated)`;
}

function htmlToTextFallback(html) {
  if (!html) {
    return '';
  }

  return String(html)
    .replace(/<style[\s\S]*?<\/style>/gi, '')
    .replace(/<script[\s\S]*?<\/script>/gi, '')
    .replace(/<[^>]+>/g, ' ')
    .replace(/\s+/g, ' ')
    .trim();
}

function env(name, fallback) {
  const value = process.env[name];
  return value === undefined || value === '' ? fallback : value;
}

function requiredEnv(name) {
  const value = process.env[name];
  if (!value) {
    throw new Error(`missing required env: ${name}`);
  }

  return value;
}

function numberEnv(name, fallback) {
  const value = env(name, String(fallback));
  const number = Number(value);
  if (!Number.isFinite(number)) {
    throw new Error(`invalid number env: ${name}=${value}`);
  }

  return number;
}

function boolEnv(name, fallback) {
  const value = env(name, String(fallback)).toLowerCase();
  if (['1', 'true', 'yes', 'on'].includes(value)) {
    return true;
  }

  if (['0', 'false', 'no', 'off'].includes(value)) {
    return false;
  }

  throw new Error(`invalid boolean env: ${name}=${value}`);
}

function fatal(error) {
  console.error('fatal error:', error?.stack || error);
  process.exit(1);
}
