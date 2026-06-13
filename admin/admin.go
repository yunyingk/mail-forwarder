package admin

import (
	"context"
	_ "embed"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/yunyingk/mail-forwarder/config"
)

//go:generate go run ../cmd/genopenapi ../api/openapi.yaml openapi.json
//go:embed openapi.json
var openAPIJSON []byte

type Server struct {
	cfg    *config.Config
	log    *slog.Logger
	server *http.Server
}

func New(cfg *config.Config, log *slog.Logger) *Server {
	mux := http.NewServeMux()
	s := &Server{
		cfg: cfg,
		log: log.With(slog.String("component", "admin")),
		server: &http.Server{
			Addr:              cfg.Admin.Listen,
			Handler:           mux,
			ReadHeaderTimeout: 5 * time.Second,
		},
	}

	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /sources", s.sources)
	mux.HandleFunc("GET /config", s.redactedConfig)
	mux.HandleFunc("GET /openapi", s.openapi)
	mux.HandleFunc("GET /openapi.json", s.openapi)
	return s
}

func (s *Server) Run() error {
	s.log.Info("admin api listening", slog.String("listen", s.server.Addr))
	err := s.server.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) sources(w http.ResponseWriter, r *http.Request) {
	type source struct {
		Name       string `json:"name"`
		Host       string `json:"host"`
		Port       int    `json:"port"`
		Secure     bool   `json:"secure"`
		User       string `json:"user"`
		Mailbox    string `json:"mailbox"`
		WebhookURL string `json:"webhook_url"`
	}

	sources := make([]source, 0, len(s.cfg.IMAP))
	for _, item := range s.cfg.IMAP {
		sources = append(sources, source{
			Name:       item.Name,
			Host:       item.Host,
			Port:       item.Port,
			Secure:     item.Secure,
			User:       item.User,
			Mailbox:    item.Mailbox,
			WebhookURL: item.Webhook.URL,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{"sources": sources})
}

func (s *Server) redactedConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, RedactConfig(s.cfg))
}

func (s *Server) openapi(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(openAPIJSON)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

type RedactedConfig struct {
	IMAP           []RedactedIMAPSource `json:"imap"`
	Admin          config.AdminConfig   `json:"admin"`
	ProcessingMode string               `json:"processing_mode"`
	State          config.StateConfig   `json:"state"`
	Retry          config.RetryConfig   `json:"retry"`
	DryRun         bool                 `json:"dry_run"`
}

type RedactedIMAPSource struct {
	Name         string                 `json:"name"`
	Host         string                 `json:"host"`
	Port         int                    `json:"port"`
	Secure       bool                   `json:"secure"`
	User         string                 `json:"user"`
	Pass         string                 `json:"pass"`
	Mailbox      string                 `json:"mailbox"`
	Webhook      RedactedWebhookConfig  `json:"webhook"`
	Timeouts     config.IMAPTimeouts    `json:"timeouts"`
	Payload      config.PayloadConfig   `json:"payload"`
	IdleFallback config.IdleFallbackOpt `json:"idle_fallback"`
}

type RedactedWebhookConfig struct {
	URL        string            `json:"url"`
	Secret     string            `json:"secret"`
	Headers    map[string]string `json:"headers,omitempty"`
	TimeoutSec int               `json:"timeout_sec"`
}

func RedactConfig(cfg *config.Config) RedactedConfig {
	result := RedactedConfig{
		Admin:          cfg.Admin,
		ProcessingMode: cfg.ProcessingMode,
		State:          cfg.State,
		Retry:          cfg.Retry,
		DryRun:         cfg.DryRun,
		IMAP:           make([]RedactedIMAPSource, 0, len(cfg.IMAP)),
	}
	for _, item := range cfg.IMAP {
		result.IMAP = append(result.IMAP, RedactedIMAPSource{
			Name:    item.Name,
			Host:    item.Host,
			Port:    item.Port,
			Secure:  item.Secure,
			User:    item.User,
			Pass:    redact(item.Pass),
			Mailbox: item.Mailbox,
			Webhook: RedactedWebhookConfig{
				URL:        item.Webhook.URL,
				Secret:     redact(item.Webhook.Secret),
				Headers:    item.Webhook.Headers,
				TimeoutSec: item.Webhook.TimeoutSec,
			},
			Timeouts:     item.Timeouts,
			Payload:      item.Payload,
			IdleFallback: item.IdleFallback,
		})
	}
	return result
}

func redact(s string) string {
	if s == "" {
		return ""
	}
	return "***"
}
