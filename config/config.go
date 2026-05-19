package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

type IMAPSource struct {
	Name     string        `yaml:"name"`
	Host     string        `yaml:"host"`
	Port     int           `yaml:"port"`
	Secure   bool          `yaml:"secure"`
	User     string        `yaml:"user"`
	Pass     string        `yaml:"pass"`
	Mailbox  string        `yaml:"mailbox"`
	Filter   IMAPFilter    `yaml:"filter"`
	Timeouts IMAPTimeouts  `yaml:"timeouts"`
}

type IMAPFilter struct {
	From          string `yaml:"from"`
	SubjectKeyword string `yaml:"subject_keyword"`
}

type IMAPTimeouts struct {
	ConnectionSec int `yaml:"connection_sec"`
	SocketSec     int `yaml:"socket_sec"`
}

type DingTalkTarget struct {
	Name    string `yaml:"name"`
	Webhook string `yaml:"webhook"`
	Secret  string `yaml:"secret"`
	Title   string `yaml:"title"`
}

type Config struct {
	IMAP          []IMAPSource     `yaml:"imap"`
	DingTalk      []DingTalkTarget `yaml:"dingtalk"`
	DryRun        bool             `yaml:"dry_run"`
	MaxTextLength int              `yaml:"max_text_length"`
	PollOnStart   bool             `yaml:"poll_on_start"`
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
	}
	for i, t := range c.DingTalk {
		if t.Name == "" {
			return fmt.Errorf("config: dingtalk[%d].name is required", i)
		}
		if t.Webhook == "" {
			return fmt.Errorf("config: dingtalk[%d].webhook is required", i)
		}
	}
	return nil
}

func (c *Config) defaults() {
	if c.MaxTextLength == 0 {
		c.MaxTextLength = 3200
	}
	for i := range c.IMAP {
		s := &c.IMAP[i]
		if s.Port == 0 {
			s.Port = 993
		}
		if s.Mailbox == "" {
			s.Mailbox = "INBOX"
		}
		if s.Filter.From != "" {
			s.Filter.From = strings.ToLower(strings.TrimSpace(s.Filter.From))
		}
		if s.Timeouts.ConnectionSec == 0 {
			s.Timeouts.ConnectionSec = 15
		}
		if s.Timeouts.SocketSec == 0 {
			s.Timeouts.SocketSec = 300
		}
	}
}
