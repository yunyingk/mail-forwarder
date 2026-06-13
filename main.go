package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/yunyingk/mail-forwarder/admin"
	"github.com/yunyingk/mail-forwarder/config"
	"github.com/yunyingk/mail-forwarder/mailer"
	"github.com/yunyingk/mail-forwarder/webhook"
)

var version = "dev"

func main() {
	configPath := flag.String("config", "config.yaml", "path to config file")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		os.Stdout.WriteString("mail-forwarder " + version + "\n")
		os.Exit(0)
	}

	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Error("load config failed", slog.Any("error", err))
		os.Exit(1)
	}

	log.Info("starting mail-forwarder",
		slog.String("version", version),
		slog.Int("imap_sources", len(cfg.IMAP)),
		slog.Bool("dry_run", cfg.DryRun),
		slog.Bool("admin_enabled", cfg.Admin.Enabled),
	)

	sender := webhook.NewSender(10 * time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	var wg sync.WaitGroup

	var adminServer *admin.Server
	if cfg.Admin.Enabled {
		adminServer = admin.New(cfg, log)
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := adminServer.Run(); err != nil {
				log.Error("admin server exited with error", slog.Any("error", err))
				cancel()
			}
		}()
	}

	for _, source := range cfg.IMAP {
		wg.Add(1)
		go func(s config.IMAPSource) {
			defer wg.Done()
			handler := func(ctx context.Context, mail mailer.Mail) (mailer.HandlerResult, error) {
				if cfg.DryRun {
					log.Info("dry-run: would post mail to webhook",
						slog.String("imap", s.Name),
						slog.String("webhook", s.Webhook.URL),
						slog.Uint64("uid", uint64(mail.UID)),
						slog.String("from", mail.From),
						slog.String("subject", mail.Subject),
					)
					return mailer.HandlerResult{MarkSeen: false}, nil
				}
				if err := sender.Send(ctx, s.Webhook, mail); err != nil {
					return mailer.HandlerResult{}, err
				}
				return mailer.HandlerResult{MarkSeen: true}, nil
			}
			l := mailer.NewListener(s, handler, cfg.PollOnStart, log)
			if err := l.Run(ctx); err != nil {
				log.Error("listener exited with error", slog.String("imap", s.Name), slog.Any("error", err))
			}
		}(source)
	}

	sig := <-sigCh
	log.Info("received signal, shutting down", slog.String("signal", sig.String()))
	cancel()
	if adminServer != nil {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		if err := adminServer.Shutdown(shutdownCtx); err != nil {
			log.Warn("admin server shutdown failed", slog.Any("error", err))
		}
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Info("all listeners stopped")
	case <-time.After(15 * time.Second):
		log.Warn("shutdown timed out after 15s")
	}
}
