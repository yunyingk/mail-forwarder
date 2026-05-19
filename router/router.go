package router

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/yunyingk/mail-forwarder/dingtalk"
	"github.com/yunyingk/mail-forwarder/mailer"
)

type Result struct {
	Targets []string
}

// Route determines which DingTalk targets should receive this mail.
// Edit this function to customize routing logic.
//
// mail contains the parsed email fields.
// availableTargets is the list of all configured DingTalk target names.
// Return a Result with the target names that should receive this mail.
func Route(mail mailer.Mail, availableTargets []string) Result {
	// Default: send to all targets.
	// Customize this to route based on mail.From, mail.Subject, mail.SourceName, etc.
	return Result{Targets: availableTargets}
}

type Dispatcher struct {
	sender  *dingtalk.Sender
	maxText int
	dryRun  bool
	log     *slog.Logger
}

func NewDispatcher(sender *dingtalk.Sender, maxText int, dryRun bool, log *slog.Logger) *Dispatcher {
	return &Dispatcher{
		sender:  sender,
		maxText: maxText,
		dryRun:  dryRun,
		log:     log,
	}
}

func (d *Dispatcher) Handle(ctx context.Context, mail mailer.Mail) error {
	result := Route(mail, d.sender.TargetNames())

	if len(result.Targets) == 0 {
		d.log.Info("no targets matched, skipping",
			slog.String("from", mail.From),
			slog.String("subject", mail.Subject),
		)
		return nil
	}

	title := mail.Subject
	body := d.buildMarkdown(mail)

	if d.dryRun {
		d.log.Info("dry-run: would send to targets",
			slog.String("from", mail.From),
			slog.String("subject", mail.Subject),
			slog.Any("targets", result.Targets),
			slog.String("body_preview", truncate(body, 200)),
		)
		return nil
	}

	for _, targetName := range result.Targets {
		if err := d.sender.Send(ctx, targetName, title, body); err != nil {
			d.log.Error("send to dingtalk failed",
				slog.String("target", targetName),
				slog.Any("error", err),
			)
			return fmt.Errorf("send to %s: %w", targetName, err)
		}
		d.log.Info("sent to dingtalk",
			slog.String("target", targetName),
			slog.String("from", mail.From),
			slog.String("subject", mail.Subject),
		)
	}

	return nil
}

func (d *Dispatcher) buildMarkdown(mail mailer.Mail) string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("### %s\n\n", mail.Subject))
	b.WriteString(fmt.Sprintf("**来源:** %s\n\n", mail.From))

	if !mail.Date.IsZero() {
		b.WriteString(fmt.Sprintf("**时间:** %s\n\n", mail.Date.Format(time.RFC3339)))
	}

	text := mail.Text
	if len(text) > d.maxText {
		text = text[:d.maxText] + "\n...(truncated)"
	}

	if text == "" {
		text = "(无正文)"
	}

	b.WriteString(text)
	return b.String()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
