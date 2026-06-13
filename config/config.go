package config

import (
	"fmt"
	"os"
	"regexp"
	"time"

	"gopkg.in/yaml.v3"
)

type IMAPSource struct {
	Name         string          `yaml:"name"`
	Host         string          `yaml:"host"`
	Port         int             `yaml:"port"`
	Secure       bool            `yaml:"secure"`
	User         string          `yaml:"user"`
	Pass         string          `yaml:"pass"`
	Mailbox      string          `yaml:"mailbox"`
	Webhook      WebhookConfig   `yaml:"webhook"`
	Timeouts     IMAPTimeouts    `yaml:"timeouts"`
	Payload      PayloadConfig   `yaml:"payload"`
	IdleFallback IdleFallbackOpt `yaml:"idle_fallback"`
}

type IMAPTimeouts struct {
	ConnectionSec int `yaml:"connection_sec"`
	SocketSec     int `yaml:"socket_sec"`
}

type WebhookConfig struct {
	URL        string            `yaml:"url"`
	Secret     string            `yaml:"secret"`
	Headers    map[string]string `yaml:"headers"`
	TimeoutSec int               `yaml:"timeout_sec"`
}

type PayloadConfig struct {
	IncludeRawRFC822 bool   `yaml:"include_raw_rfc822"`
	Attachments      string `yaml:"attachments"`
}

type IdleFallbackOpt struct {
	Allow       bool `yaml:"allow"`
	IntervalSec int  `yaml:"interval_sec"`
}

type Config struct {
	IMAP           []IMAPSource `yaml:"imap"`
	Admin          AdminConfig  `yaml:"admin"`
	ProcessingMode string       `yaml:"processing_mode"`
	State          StateConfig  `yaml:"state"`
	Retry          RetryConfig  `yaml:"retry"`
	DryRun         bool         `yaml:"dry_run"`
}

type AdminConfig struct {
	Enabled bool   `yaml:"enabled"`
	Listen  string `yaml:"listen"`
}

type StateConfig struct {
	Path string `yaml:"path"`
}

type RetryConfig struct {
	Backoff []Duration `yaml:"backoff"`
}

type Duration struct {
	time.Duration
}

func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return err
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	d.Duration = parsed
	return nil
}

func (d Duration) MarshalYAML() (interface{}, error) {
	return d.String(), nil
}

var envVarRe = regexp.MustCompile(`\$\{([^}]+)}`)

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	expanded := expandEnvVars(string(data))

	var cfg Config
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	cfg.defaults()
	return &cfg, nil
}

func expandEnvVars(s string) string {
	return envVarRe.ReplaceAllStringFunc(s, func(match string) string {
		key := match[2 : len(match)-1]
		return os.Getenv(key)
	})
}

func (c *Config) validate() error {
	if len(c.IMAP) == 0 {
		return fmt.Errorf("config: at least one imap source is required")
	}
	switch c.ProcessingMode {
	case "unread_queue", "new_unread_queue", "checkpoint_from_now", "checkpoint_from_unread":
	case "":
		return fmt.Errorf("config: processing_mode is required; choose one of unread_queue, new_unread_queue, checkpoint_from_now, checkpoint_from_unread")
	default:
		return fmt.Errorf("config: processing_mode %q is invalid; choose one of unread_queue, new_unread_queue, checkpoint_from_now, checkpoint_from_unread", c.ProcessingMode)
	}
	if c.State.Path == "" {
		return fmt.Errorf("config: state.path is required")
	}
	for i, s := range c.IMAP {
		if s.Host == "" {
			return fmt.Errorf("config: imap[%d].host is required", i)
		}
		if s.User == "" {
			return fmt.Errorf("config: imap[%d].user is required", i)
		}
		if s.Pass == "" {
			return fmt.Errorf("config: imap[%d].pass is required", i)
		}
		if s.Webhook.URL == "" {
			return fmt.Errorf("config: imap[%d].webhook.url is required", i)
		}
		switch s.Payload.Attachments {
		case "", "disabled", "metadata", "inline_base64":
		default:
			return fmt.Errorf("config: imap[%d].payload.attachments must be disabled, metadata, or inline_base64", i)
		}
	}
	return nil
}

func (c *Config) defaults() {
	if c.Admin.Listen == "" {
		c.Admin.Listen = "127.0.0.1:6245"
	}
	if len(c.Retry.Backoff) == 0 {
		c.Retry.Backoff = []Duration{
			{Duration: 5 * time.Minute},
			{Duration: 30 * time.Minute},
			{Duration: 2 * time.Hour},
			{Duration: 6 * time.Hour},
			{Duration: 24 * time.Hour},
		}
	}
	for i := range c.IMAP {
		s := &c.IMAP[i]
		if s.Port == 0 {
			s.Port = 993
		}
		if s.Mailbox == "" {
			s.Mailbox = "INBOX"
		}
		if s.Timeouts.ConnectionSec == 0 {
			s.Timeouts.ConnectionSec = 15
		}
		if s.Timeouts.SocketSec == 0 {
			s.Timeouts.SocketSec = 300
		}
		if s.Webhook.TimeoutSec == 0 {
			s.Webhook.TimeoutSec = 10
		}
		if s.Payload.Attachments == "" {
			s.Payload.Attachments = "disabled"
		}
		if s.IdleFallback.IntervalSec == 0 {
			s.IdleFallback.IntervalSec = 60
		}
	}
}
