package cmd

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"padel-cli/storage"
)

type Notifier interface {
	Notify(ctx context.Context, message string) error
}

type noopNotifier struct{}

func (noopNotifier) Notify(ctx context.Context, message string) error {
	return nil
}

type telegramNotifier struct {
	botToken string
	chatID   string
	client   *http.Client
}

// newNotifierFromFile loads Telegram credentials from ~/.config/padel/telegram.json
// and returns a ready notifier. Returns noopNotifier (not an error) when the
// file is absent or unconfigured, so callers that treat Telegram as optional
// can use the warnIfUnconfigured flag to print a console warning instead.
func newNotifierFromFile(warnIfUnconfigured bool) (Notifier, error) {
	tgCfg, err := storage.LoadTelegramConfig()
	if err != nil {
		return nil, fmt.Errorf("load telegram config: %w", err)
	}
	if !tgCfg.IsConfigured() {
		if warnIfUnconfigured {
			fmt.Println("warning: Telegram not configured (~/.config/padel/telegram.json missing or incomplete); printing alerts to console only")
		}
		return noopNotifier{}, nil
	}
	return newTelegramNotifier(tgCfg.BotToken, tgCfg.ChatID)
}

// newNotifier is kept for auto-book, which declares telegram enabled/disabled
// via its YAML config. It now reads credentials from telegram.json rather than
// env vars, ignoring the legacy BotTokenEnv/ChatIDEnv fields.
func newNotifier(cfg AutoBookNotificationsConfig) (Notifier, error) {
	if cfg.Telegram.Enabled {
		return newNotifierFromFile(false)
	}
	return noopNotifier{}, nil
}

// newTelegramNotifier builds a Telegram notifier from a raw token and chat id.
// Both must be non-empty. Used by newNotifierFromFile.
func newTelegramNotifier(token, chatID string) (Notifier, error) {
	token = strings.TrimSpace(token)
	chatID = strings.TrimSpace(chatID)
	if token == "" {
		return nil, fmt.Errorf("telegram bot token is empty")
	}
	if chatID == "" {
		return nil, fmt.Errorf("telegram chat id is empty")
	}
	return &telegramNotifier{
		botToken: token,
		chatID:   chatID,
		client:   &http.Client{Timeout: 10 * time.Second},
	}, nil
}

func (n *telegramNotifier) Notify(ctx context.Context, message string) error {
	form := url.Values{}
	form.Set("chat_id", n.chatID)
	form.Set("text", message)
	form.Set("disable_web_page_preview", "true")

	endpoint := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", n.botToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := n.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("telegram notification failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}
