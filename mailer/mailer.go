package mailer

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"github.com/emersion/go-message/mail"
	"github.com/yunyingk/mail-forwarder/config"
)

type Mail struct {
	SourceName        string
	Mailbox           string
	UID               uint32
	From              string
	To                []string
	Cc                []string
	ReplyTo           []string
	Subject           string
	Date              time.Time
	Text              string
	HTML              string
	MessageID         string
	Headers           map[string]string
	Attachments       []Attachment
	RawRFC822Base64   string
	RawRFC822Size     int
	RawRFC822Included bool
}

type Attachment struct {
	Filename      string
	ContentType   string
	ContentID     string
	Disposition   string
	Size          int
	ContentBase64 string
}

type HandlerResult struct {
	MarkSeen bool
}

type Handler func(ctx context.Context, mail Mail) (HandlerResult, error)

type Listener struct {
	source      config.IMAPSource
	handler     Handler
	pollOnStart bool
	log         *slog.Logger
}

func NewListener(source config.IMAPSource, handler Handler, pollOnStart bool, log *slog.Logger) *Listener {
	return &Listener{
		source:      source,
		handler:     handler,
		pollOnStart: pollOnStart,
		log: log.With(
			slog.String("imap", source.Name),
			slog.String("host", source.Host),
			slog.String("user", source.User),
		),
	}
}

func (l *Listener) Run(ctx context.Context) error {
	for {
		if err := l.connectAndListen(ctx); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			l.log.Error("imap connection error, reconnecting in 10s", slog.Any("error", err))
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(10 * time.Second):
			}
		}
	}
}

func (l *Listener) connectAndListen(ctx context.Context) error {
	addr := net.JoinHostPort(l.source.Host, fmt.Sprintf("%d", l.source.Port))
	timeout := time.Duration(l.source.Timeouts.ConnectionSec) * time.Second

	var conn net.Conn
	var err error

	if l.source.Secure {
		dialer := &net.Dialer{Timeout: timeout}
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, nil)
	} else {
		conn, err = net.DialTimeout("tcp", addr, timeout)
	}
	if err != nil {
		return fmt.Errorf("dial %s: %w", addr, err)
	}

	c, err := client.New(conn)
	if err != nil {
		conn.Close()
		return fmt.Errorf("new imap client: %w", err)
	}
	defer c.Logout()

	if err := c.Login(l.source.User, l.source.Pass); err != nil {
		return fmt.Errorf("login: %w", err)
	}

	l.log.Info("imap connected")

	mbox, err := c.Select(l.source.Mailbox, false)
	if err != nil {
		return fmt.Errorf("select mailbox %s: %w", l.source.Mailbox, err)
	}

	l.log.Info("mailbox opened",
		slog.String("mailbox", l.source.Mailbox),
		slog.Uint64("messages", uint64(mbox.Messages)),
	)

	if l.pollOnStart {
		if err := l.processUnread(ctx, c); err != nil {
			return err
		}
	}

	if err := l.listenIdle(ctx, c); err != nil {
		return err
	}

	return nil
}

func (l *Listener) listenIdle(ctx context.Context, c *client.Client) error {
	updates := make(chan client.Update, 16)
	c.Updates = updates

	for {
		stop := make(chan struct{})
		idleDone := make(chan error, 1)
		go func() {
			opts := &client.IdleOptions{PollInterval: -1}
			if l.source.IdleFallback.Allow {
				opts.PollInterval = time.Duration(l.source.IdleFallback.IntervalSec) * time.Second
			}
			idleDone <- c.Idle(stop, opts)
		}()

		shouldProcess := false
		select {
		case <-ctx.Done():
			close(stop)
			<-idleDone
			return nil
		case _, ok := <-updates:
			close(stop)
			if err := <-idleDone; err != nil {
				return fmt.Errorf("idle: %w", err)
			}
			if !ok {
				return fmt.Errorf("imap updates channel closed")
			}
			shouldProcess = true
		case err := <-idleDone:
			if err != nil {
				return fmt.Errorf("idle: %w", err)
			}
			shouldProcess = true
		}

		if shouldProcess {
			if err := l.processUnread(ctx, c); err != nil {
				return err
			}
		}
	}
}

func (l *Listener) processUnread(ctx context.Context, c *client.Client) error {
	criteria := imap.NewSearchCriteria()
	criteria.WithoutFlags = []string{imap.SeenFlag}

	uids, err := c.UidSearch(criteria)
	if err != nil {
		return fmt.Errorf("search: %w", err)
	}

	if len(uids) == 0 {
		return nil
	}

	l.log.Info("found unread messages", slog.Int("count", len(uids)))

	for _, uid := range uids {
		if ctx.Err() != nil {
			return nil
		}
		if err := l.processOne(ctx, c, uid); err != nil {
			l.log.Error("process message failed", slog.Uint64("uid", uint64(uid)), slog.Any("error", err))
		}
	}

	return nil
}

func (l *Listener) processOne(ctx context.Context, c *client.Client, uid uint32) error {
	seqSet := new(imap.SeqSet)
	seqSet.AddNum(uid)

	section := &imap.BodySectionName{}
	messages := make(chan *imap.Message, 1)

	done := make(chan error, 1)
	go func() {
		done <- c.UidFetch(seqSet, []imap.FetchItem{imap.FetchEnvelope, section.FetchItem()}, messages)
	}()

	msg := <-messages
	if err := <-done; err != nil {
		return fmt.Errorf("fetch uid %d: %w", uid, err)
	}

	if msg == nil {
		return nil
	}

	env := msg.Envelope
	from := firstAddress(env.From)

	subject := env.Subject

	r := msg.GetBody(section)
	if r == nil {
		return fmt.Errorf("no body for uid %d", uid)
	}

	raw, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("read body uid %d: %w", uid, err)
	}
	parsed := parseMail(bytes.NewReader(raw), env, l.source.Payload.Attachments)

	m := Mail{
		SourceName:        l.source.Name,
		Mailbox:           l.source.Mailbox,
		UID:               uid,
		From:              from,
		To:                addresses(env.To),
		Cc:                addresses(env.Cc),
		ReplyTo:           addresses(env.ReplyTo),
		Subject:           subject,
		Date:              parsed.Date,
		Text:              parsed.Text,
		HTML:              parsed.HTML,
		MessageID:         parsed.MessageID,
		Headers:           parsed.Headers,
		Attachments:       parsed.Attachments,
		RawRFC822Size:     len(raw),
		RawRFC822Included: l.source.Payload.IncludeRawRFC822,
	}
	if l.source.Payload.IncludeRawRFC822 {
		m.RawRFC822Base64 = base64.StdEncoding.EncodeToString(raw)
	}

	result, err := l.handler(ctx, m)
	if err != nil {
		return err
	}
	if !result.MarkSeen {
		l.log.Info("forwarded without marking seen",
			slog.Uint64("uid", uint64(uid)),
			slog.String("from", from),
			slog.String("subject", subject),
		)
		return nil
	}

	if err := l.markSeen(c, uid); err != nil {
		return fmt.Errorf("mark seen uid %d: %w", uid, err)
	}

	l.log.Info("forwarded and marked seen",
		slog.Uint64("uid", uint64(uid)),
		slog.String("from", from),
		slog.String("subject", subject),
	)
	return nil
}

func (l *Listener) markSeen(c *client.Client, uid uint32) error {
	seqSet := new(imap.SeqSet)
	seqSet.AddNum(uid)
	item := imap.FormatFlagsOp(imap.AddFlags, true)
	return c.UidStore(seqSet, item, []interface{}{imap.SeenFlag}, nil)
}

type parsedMail struct {
	Text        string
	HTML        string
	MessageID   string
	Date        time.Time
	Headers     map[string]string
	Attachments []Attachment
}

func parseMail(r io.Reader, env *imap.Envelope, attachmentMode string) parsedMail {
	parsed := parsedMail{
		Date:      env.Date,
		MessageID: env.MessageId,
		Headers:   make(map[string]string),
	}

	entity, err := mail.CreateReader(r)
	if err != nil {
		return parsed
	}

	for _, key := range []string{
		"Message-Id",
		"Return-Path",
		"Authentication-Results",
		"DKIM-Signature",
		"Content-Type",
		"MIME-Version",
	} {
		if value := entity.Header.Get(key); value != "" {
			parsed.Headers[strings.ToLower(key)] = value
		}
	}

	if messageID := entity.Header.Get("Message-Id"); messageID != "" {
		parsed.MessageID = messageID
	}
	if d, err := entity.Header.Date(); err == nil {
		parsed.Date = d
	}

	for {
		p, err := entity.NextPart()
		if err != nil {
			break
		}

		switch h := p.Header.(type) {
		case *mail.InlineHeader:
			ct, _, _ := h.ContentType()
			if strings.HasPrefix(ct, "text/plain") {
				body, _ := io.ReadAll(p.Body)
				if parsed.Text == "" {
					parsed.Text = string(body)
				}
			}
			if strings.HasPrefix(ct, "text/html") {
				body, _ := io.ReadAll(p.Body)
				if parsed.HTML == "" {
					parsed.HTML = string(body)
				}
			}
		case *mail.AttachmentHeader:
			attachment := Attachment{
				ContentID:   h.Get("Content-Id"),
				Disposition: h.Get("Content-Disposition"),
			}
			if filename, err := h.Filename(); err == nil {
				attachment.Filename = filename
			}
			if ct, _, err := h.ContentType(); err == nil {
				attachment.ContentType = ct
			}
			body, _ := io.ReadAll(p.Body)
			attachment.Size = len(body)
			if attachmentMode == "inline_base64" {
				attachment.ContentBase64 = base64.StdEncoding.EncodeToString(body)
			}
			if attachmentMode == "metadata" || attachmentMode == "inline_base64" {
				parsed.Attachments = append(parsed.Attachments, attachment)
			}
		}
	}
	return parsed
}

func firstAddress(addrs []*imap.Address) string {
	if len(addrs) == 0 {
		return ""
	}
	a := addrs[0]
	if a.HostName == "" {
		return ""
	}
	mailbox := a.MailboxName
	if mailbox == "" {
		return ""
	}
	return strings.ToLower(mailbox + "@" + a.HostName)
}

func addresses(addrs []*imap.Address) []string {
	result := make([]string, 0, len(addrs))
	for _, a := range addrs {
		if a == nil || a.HostName == "" || a.MailboxName == "" {
			continue
		}
		result = append(result, strings.ToLower(a.MailboxName+"@"+a.HostName))
	}
	return result
}
