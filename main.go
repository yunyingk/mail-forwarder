package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/yunyingk/mail-forwarder/admin"
	"github.com/yunyingk/mail-forwarder/config"
	"github.com/yunyingk/mail-forwarder/mailer"
	statepkg "github.com/yunyingk/mail-forwarder/state"
	"github.com/yunyingk/mail-forwarder/webhook"
)

var version = "dev"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "init-config" {
		initConfig(os.Args[2:])
		return
	}

	flags := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	configPath := flags.String("config", "config.yaml", "path to config file")
	showVersion := flags.Bool("version", false, "print version and exit")
	flags.Parse(os.Args[1:])

	if *showVersion {
		os.Stdout.WriteString("mail-forwarder " + version + "\n")
		os.Exit(0)
	}

	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	cfg, err := config.Load(*configPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			log.Error("config file not found",
				slog.String("path", *configPath),
				slog.String("hint", "run: mail-forwarder init-config"),
			)
			os.Exit(1)
		}
		log.Error("load config failed", slog.Any("error", err))
		os.Exit(1)
	}

	log.Info("starting mail-forwarder",
		slog.String("version", version),
		slog.Int("imap_sources", len(cfg.IMAP)),
		slog.String("processing_mode", cfg.ProcessingMode),
		slog.Bool("dry_run", cfg.DryRun),
		slog.Bool("admin_enabled", cfg.Admin.Enabled),
	)

	sender := webhook.NewSender(10 * time.Second)
	store, err := statepkg.Open(cfg.State.Path)
	if err != nil {
		log.Error("open state failed", slog.Any("error", err))
		os.Exit(1)
	}
	backoff := make([]time.Duration, 0, len(cfg.Retry.Backoff))
	for _, d := range cfg.Retry.Backoff {
		backoff = append(backoff, d.Duration)
	}

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
			l := mailer.NewListener(s, handler, cfg.ProcessingMode, store, backoff, log)
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

func initConfig(args []string) {
	flags := flag.NewFlagSet("init-config", flag.ExitOnError)
	output := flags.String("output", "config.yaml", "path to write starter config")
	force := flags.Bool("force", false, "overwrite existing config")
	flags.Parse(args)

	if !*force {
		if _, err := os.Stat(*output); err == nil {
			fmt.Fprintf(os.Stderr, "%s already exists; refusing to overwrite\n", *output)
			os.Exit(1)
		} else if !errors.Is(err, os.ErrNotExist) {
			fmt.Fprintf(os.Stderr, "check output: %v\n", err)
			os.Exit(1)
		}
	}

	if err := os.WriteFile(*output, []byte(config.StarterConfig), 0600); err != nil {
		fmt.Fprintf(os.Stderr, "write config: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stdout, "created %s\n", *output)
	fmt.Fprintln(os.Stdout, "edit it, then run: mail-forwarder -config "+*output)
}
