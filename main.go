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

	"github.com/yunyingk/mail-forwarder/config"
	"github.com/yunyingk/mail-forwarder/dingtalk"
	"github.com/yunyingk/mail-forwarder/mailer"
	"github.com/yunyingk/mail-forwarder/router"
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
		slog.Int("dingtalk_targets", len(cfg.DingTalk)),
		slog.Bool("dry_run", cfg.DryRun),
	)

	sender := dingtalk.NewSender(cfg.DingTalk, 10*time.Second)
	dispatcher := router.NewDispatcher(sender, cfg.MaxTextLength, cfg.DryRun, log)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	var wg sync.WaitGroup

	for _, source := range cfg.IMAP {
		wg.Add(1)
		go func(s config.IMAPSource) {
			defer wg.Done()
			l := mailer.NewListener(s, dispatcher.Handle, log)
			if err := l.Run(ctx); err != nil {
				log.Error("listener exited with error", slog.String("imap", s.Name), slog.Any("error", err))
			}
		}(source)
	}

	sig := <-sigCh
	log.Info("received signal, shutting down", slog.String("signal", sig.String()))
	cancel()

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
