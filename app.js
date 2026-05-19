import crypto from 'node:crypto';
import process from 'node:process';
import axios from 'axios';
import { ImapFlow } from 'imapflow';
import { simpleParser } from 'mailparser';

const config = {
  imapHost: requiredEnv('IMAP_HOST'),
  imapPort: numberEnv('IMAP_PORT', 993),
  imapSecure: boolEnv('IMAP_SECURE', true),
  imapUser: requiredEnv('IMAP_USER'),
  imapPass: requiredEnv('IMAP_PASS'),
  mailbox: env('MAILBOX', 'INBOX'),
  filterFrom: normalizeEmail(requiredEnv('FILTER_FROM')),
  filterSubjectKeyword: env('FILTER_SUBJECT_KEYWORD', ''),
  dingtalkWebhook: requiredEnv('DINGTALK_WEBHOOK'),
  dingtalkSecret: env('DINGTALK_SECRET', ''),
  httpTimeoutMs: numberEnv('HTTP_TIMEOUT_MS', 10000),
  pollOnStart: boolEnv('POLL_ON_START', true),
  maxTextLength: numberEnv('MAX_TEXT_LENGTH', 3200)
};

let shuttingDown = false;
let processing = false;

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
  console.log(`starting mail-forwarder for ${config.imapUser}, mailbox=${config.mailbox}`);

  const client = new ImapFlow({
    host: config.imapHost,
    port: config.imapPort,
    secure: config.imapSecure,
    auth: {
      user: config.imapUser,
      pass: config.imapPass
    },
    logger: false
  });

  client.on('error', (error) => {
    fatal(error);
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
      await client.idle();
      await processUnread(client);
    }
  } finally {
    lock.release();
    await client.logout().catch(() => {});
  }
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
  const ekuaibao = parseEkuaibaoFailureMail(text);
  const payload = buildDingTalkPayload({
    uid,
    messageId: parsed.messageId || summary.envelope?.messageId || message.envelope?.messageId || '',
    from,
    subject: parsed.subject || summary.envelope?.subject || message.envelope?.subject || '',
    date: parsed.date || summary.envelope?.date || message.envelope?.date || null,
    text,
    ekuaibao
  });

  try {
    await postToDingTalk(payload);
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
  const content = buildDingTalkText(mail, body, date);

  return {
    dingtalk: {
      msgtype: 'text',
      text: {
        content
      }
    },
    meta: {
      uid: mail.uid,
      messageId: mail.messageId,
      from: mail.from,
      subject: mail.subject,
      date,
      text: body,
      ekuaibao: mail.ekuaibao
    }
  };
}

function buildDingTalkText(mail, body, date) {
  const workflow = mail.ekuaibao?.workflowName;
  const result = mail.ekuaibao?.result;
  const runTime = mail.ekuaibao?.runTime;
  const runId = mail.ekuaibao?.runId;
  const failedNodes = mail.ekuaibao?.failedNodes || [];

  if (!workflow && !runId && !failedNodes.length) {
    return [
      '合思工作流失败提醒',
      `发件人: ${mail.from}`,
      `主题: ${mail.subject || '(无主题)'}`,
      date ? `邮件时间: ${date}` : '',
      '',
      body || '(无正文)'
    ].filter(Boolean).join('\n');
  }

  const lines = [
    '合思工作流失败提醒',
    workflow ? `企业/工作流: ${workflow}` : '',
    result ? `流程运行结果: ${result}` : '',
    runTime ? `运行时间: ${runTime}` : '',
    runId ? `运行ID: ${runId}` : '',
    ''
  ].filter(Boolean);

  if (failedNodes.length) {
    lines.push('失败节点:');
    for (const node of failedNodes) {
      lines.push(`- ${node.name}${node.action ? ` (${node.action})` : ''}`);
    }
    lines.push('');
  }

  lines.push('原始邮件摘要:');
  lines.push(trimText(body, 1200) || '(无正文)');
  return lines.join('\n');
}

function parseEkuaibaoFailureMail(text) {
  const normalized = String(text || '')
    .replace(/\r\n/g, '\n')
    .replace(/[“”]/g, '"')
    .replace(/[「」]/g, '"');

  const workflowName = matchFirst(normalized, /工作流\s*"([^"]+)"/);
  const result = matchFirst(normalized, /流程运行结果[:：]\s*"([^"]+)"/);
  const runTime = matchFirst(normalized, /运行时间[:：]\s*"([^"]+)"/);
  const runId = matchFirst(normalized, /运行ID[:：]\s*"([^"]+)"/);
  const failedNodes = [];
  const nodePattern = /失败节点[:：]\s*"([^"]+)"(?:\n|\s)+处理方式[:：]\s*"([^"]+)"/g;

  for (const match of normalized.matchAll(nodePattern)) {
    failedNodes.push({
      name: match[1].trim(),
      action: match[2].trim()
    });
  }

  return {
    workflowName,
    result,
    runTime,
    runId,
    failedNodes
  };
}

function matchFirst(text, pattern) {
  const match = text.match(pattern);
  return match?.[1]?.trim() || '';
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
