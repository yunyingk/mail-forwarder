package mailer

import (
	"context"
	"crypto/tls"
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
	SourceName string
	UID        uint32
	From       string
	Subject    string
	Date       time.Time
	Text       string
	MessageID  string
}

type Handler func(ctx context.Context, mail Mail) error

type Listener struct {
	source  config.IMAPSource
	handler Handler
	log     *slog.Logger
}

func NewListener(source config.IMAPSource, handler Handler, log *slog.Logger) *Listener {
	return &Listener{
		source:  source,
		handler: handler,
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

	if err := l.processUnread(ctx, c); err != nil {
		return err
	}

	updates := make(chan client.Update, 16)
	c.Updates = updates

	go l.handleUpdates(ctx, c, updates)

	stop := make(chan struct{})
	go func() {
		<-ctx.Done()
		close(stop)
	}()

	if err := c.Idle(stop, nil); err != nil {
		return fmt.Errorf("idle: %w", err)
	}

	return nil
}

func (l *Listener) handleUpdates(ctx context.Context, c *client.Client, updates chan client.Update) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-updates:
			if err := l.processUnread(ctx, c); err != nil {
				l.log.Error("process unread failed", slog.Any("error", err))
			}
		}
	}
}

func (l *Listener) processUnread(ctx context.Context, c *client.Client) error {
	criteria := imap.NewSearchCriteria()
	criteria.WithoutFlags = []string{imap.SeenFlag}

	uids, err := c.Search(criteria)
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
		done <- c.Fetch(seqSet, []imap.FetchItem{imap.FetchEnvelope, section.FetchItem()}, messages)
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

	if l.source.Filter.From != "" && !strings.EqualFold(from, l.source.Filter.From) {
		return nil
	}

	subject := env.Subject
	if l.source.Filter.SubjectKeyword != "" && !strings.Contains(subject, l.source.Filter.SubjectKeyword) {
		return nil
	}

	r := msg.GetBody(section)
	if r == nil {
		return fmt.Errorf("no body for uid %d", uid)
	}

	text, messageID, date := parseMail(r, env)

	m := Mail{
		SourceName: l.source.Name,
		UID:        uid,
		From:       from,
		Subject:    subject,
		Date:       date,
		Text:       text,
		MessageID:  messageID,
	}

	if err := l.handler(ctx, m); err != nil {
		return err
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
	return c.Store(seqSet, item, []interface{}{imap.SeenFlag}, nil)
}

func parseMail(r io.Reader, env *imap.Envelope) (text string, messageID string, date time.Time) {
	date = env.Date

	entity, err := mail.CreateReader(r)
	if err != nil {
		text = ""
		messageID = env.MessageId
		return
	}

	messageID = entity.Header.Get("Message-Id")
	if messageID == "" {
		messageID = env.MessageId
	}

	if d, err := entity.Header.Date(); err == nil {
		date = d
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
				text = string(body)
				return
			}
			if strings.HasPrefix(ct, "text/html") && text == "" {
				body, _ := io.ReadAll(p.Body)
				text = htmlToText(string(body))
			}
		}
	}
	return
}

func htmlToText(html string) string {
	s := html
	for _, tag := range []string{"style", "script"} {
		for {
			start := strings.Index(strings.ToLower(s), "<"+tag)
			if start == -1 {
				break
			}
			end := strings.Index(strings.ToLower(s[start:]), "</"+tag+">")
			if end == -1 {
				break
			}
			s = s[:start] + s[start+end+len(tag)+3:]
		}
	}

	var b strings.Builder
	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			b.WriteRune(r)
		}
	}

	result := b.String()
	result = strings.ReplaceAll(result, "&nbsp;", " ")
	result = strings.ReplaceAll(result, "&amp;", "&")
	result = strings.ReplaceAll(result, "&lt;", "<")
	result = strings.ReplaceAll(result, "&gt;", ">")
	result = strings.ReplaceAll(result, "&quot;", "\"")

	lines := strings.Split(result, "\n")
	var filtered []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			filtered = append(filtered, line)
		}
	}
	return strings.Join(filtered, "\n")
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
