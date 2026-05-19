package dingtalk

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/yunyingk/mail-forwarder/config"
)

type Sender struct {
	client  *http.Client
	targets map[string]config.DingTalkTarget
}

func NewSender(targets []config.DingTalkTarget, timeout time.Duration) *Sender {
	m := make(map[string]config.DingTalkTarget, len(targets))
	for _, t := range targets {
		m[t.Name] = t
	}
	return &Sender{
		client:  &http.Client{Timeout: timeout},
		targets: m,
	}
}

func (s *Sender) TargetNames() []string {
	names := make([]string, 0, len(s.targets))
	for name := range s.targets {
		names = append(names, name)
	}
	return names
}

func (s *Sender) Send(ctx context.Context, targetName string, title string, markdownBody string) error {
	t, ok := s.targets[targetName]
	if !ok {
		return fmt.Errorf("dingtalk target %q not found", targetName)
	}

	payload := map[string]interface{}{
		"msgtype": "markdown",
		"markdown": map[string]string{
			"title": title,
			"text":  markdownBody,
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	webhookURL := signURL(t.Webhook, t.Secret)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("post to dingtalk: %w", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("dingtalk returned status %d", resp.StatusCode)
	}

	return nil
}

func signURL(webhook string, secret string) string {
	if secret == "" {
		return webhook
	}

	timestamp := time.Now().UnixMilli()
	stringToSign := fmt.Sprintf("%d\n%s", timestamp, secret)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(stringToSign))
	sign := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	u, err := url.Parse(webhook)
	if err != nil {
		return webhook
	}

	q := u.Query()
	q.Set("timestamp", strconv.FormatInt(timestamp, 10))
	q.Set("sign", sign)
	u.RawQuery = q.Encode()

	return u.String()
}
