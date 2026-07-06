package storage

import (
	"encoding/json"
	"fmt"
	"os"
)

const telegramFile = "telegram.json"

// TelegramConfig holds the credentials for the Telegram bot.
// Stored at ~/.config/padel/telegram.json (or PADEL_CONFIG_DIR).
type TelegramConfig struct {
	BotToken string `json:"bot_token"`
	ChatID   string `json:"chat_id"`
}

func TelegramConfigPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s/%s", dir, telegramFile), nil
}

// LoadTelegramConfig reads telegram.json. Returns an empty config (not an
// error) when the file doesn't exist yet, so callers can check IsConfigured().
func LoadTelegramConfig() (TelegramConfig, error) {
	path, err := TelegramConfigPath()
	if err != nil {
		return TelegramConfig{}, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return TelegramConfig{}, nil
	}
	if err != nil {
		return TelegramConfig{}, err
	}
	var cfg TelegramConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return TelegramConfig{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return cfg, nil
}

func SaveTelegramConfig(cfg TelegramConfig) error {
	if _, err := ensureConfigDir(); err != nil {
		return err
	}
	path, err := TelegramConfigPath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

func (c TelegramConfig) IsConfigured() bool {
	return c.BotToken != "" && c.ChatID != ""
}
