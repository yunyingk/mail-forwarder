package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/yunyingk/mail-forwarder/config"
	"github.com/yunyingk/mail-forwarder/mailer"
)

type Sender struct {
	client *http.Client
}

type Payload struct {
	Source  SourcePayload  `json:"source"`
	Message MessagePayload `json:"message"`
}

type SourcePayload struct {
	Name    string `json:"name"`
	Mailbox string `json:"mailbox"`
}

type MessagePayload struct {
	UID         uint32              `json:"uid"`
	MessageID   string              `json:"message_id"`
	Date        string              `json:"date,omitempty"`
	Subject     string              `json:"subject"`
	From        string              `json:"from"`
	To          []string            `json:"to,omitempty"`
	Cc          []string            `json:"cc,omitempty"`
	ReplyTo     []string            `json:"reply_to,omitempty"`
	Headers     map[string]string   `json:"headers,omitempty"`
	Bodies      BodiesPayload       `json:"bodies"`
	Attachments []AttachmentPayload `json:"attachments,omitempty"`
	Raw         *RawPayload         `json:"raw,omitempty"`
}

type BodiesPayload struct {
	Text string `json:"text,omitempty"`
	HTML string `json:"html,omitempty"`
}

type AttachmentPayload struct {
	Filename      string `json:"filename,omitempty"`
	ContentType   string `json:"content_type,omitempty"`
	ContentID     string `json:"content_id,omitempty"`
	Disposition   string `json:"disposition,omitempty"`
	Size          int    `json:"size"`
	ContentBase64 string `json:"content_base64,omitempty"`
}

type RawPayload struct {
	RFC822Base64 string `json:"rfc822_base64,omitempty"`
	Size         int    `json:"size"`
	Included     bool   `json:"included"`
}

func NewSender(timeout time.Duration) *Sender {
	return &Sender{
		client: &http.Client{Timeout: timeout},
	}
}

func (s *Sender) Send(ctx context.Context, target config.WebhookConfig, mail mailer.Mail) error {
	body, err := json.Marshal(BuildPayload(mail))
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target.URL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "mail-forwarder")
	for k, v := range target.Headers {
		req.Header.Set(k, v)
	}
	sign(req, body, target.Secret)

	client := s.client
	if target.TimeoutSec > 0 {
		client = &http.Client{Timeout: time.Duration(target.TimeoutSec) * time.Second}
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("post webhook: %w", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}

	return nil
}

func BuildPayload(mail mailer.Mail) Payload {
	msg := MessagePayload{
		UID:         mail.UID,
		MessageID:   mail.MessageID,
		Subject:     mail.Subject,
		From:        mail.From,
		To:          mail.To,
		Cc:          mail.Cc,
		ReplyTo:     mail.ReplyTo,
		Headers:     mail.Headers,
		Attachments: attachments(mail.Attachments),
		Bodies: BodiesPayload{
			Text: mail.Text,
			HTML: mail.HTML,
		},
	}
	if !mail.Date.IsZero() {
		msg.Date = mail.Date.Format(time.RFC3339)
	}
	if mail.RawRFC822Included || mail.RawRFC822Size > 0 {
		msg.Raw = &RawPayload{
			RFC822Base64: mail.RawRFC822Base64,
			Size:         mail.RawRFC822Size,
			Included:     mail.RawRFC822Included,
		}
	}

	return Payload{
		Source: SourcePayload{
			Name:    mail.SourceName,
			Mailbox: mail.Mailbox,
		},
		Message: msg,
	}
}

func attachments(items []mailer.Attachment) []AttachmentPayload {
	if len(items) == 0 {
		return nil
	}
	result := make([]AttachmentPayload, 0, len(items))
	for _, item := range items {
		result = append(result, AttachmentPayload{
			Filename:      item.Filename,
			ContentType:   item.ContentType,
			ContentID:     item.ContentID,
			Disposition:   item.Disposition,
			Size:          item.Size,
			ContentBase64: item.ContentBase64,
		})
	}
	return result
}

func sign(req *http.Request, body []byte, secret string) {
	if secret == "" {
		return
	}

	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body)
	signature := hex.EncodeToString(mac.Sum(nil))

	req.Header.Set("X-Mail-Forwarder-Timestamp", timestamp)
	req.Header.Set("X-Mail-Forwarder-Signature", "sha256="+signature)
}
