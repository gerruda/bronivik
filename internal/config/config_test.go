package config

import (
	"os"
	"path/filepath"
	"testing"

	"bronivik/internal/models"
)

func TestLoadConfig(t *testing.T) {
	// Create a temporary config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	yamlContent := `
telegram:
  bot_token: "test_token"
database:
  path: "test.db"
items:
  - id: 1
    name: "Item 1"
    total_quantity: 1
`
	if err := os.WriteFile(configPath, []byte(yamlContent), 0o644); err != nil {
		t.Fatalf("failed to write temp config: %v", err)
	}

	// Mock .env file
	if err := os.WriteFile(".env", []byte(""), 0o644); err != nil {
		t.Fatalf("failed to write .env: %v", err)
	}
	defer os.Remove(".env")

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if cfg.Telegram.BotToken != "test_token" {
		t.Errorf("expected bot_token test_token, got %s", cfg.Telegram.BotToken)
	}

	if len(cfg.Items) != 1 || cfg.Items[0].ID != 1 {
		t.Errorf("expected 1 item with ID 1")
	}
}

func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name: "valid config",
			cfg: Config{
				Telegram: TelegramConfig{BotToken: "token"},
				Database: DatabaseConfig{Path: "path"},
				Items:    []models.Item{{ID: 1, Name: "Item 1"}},
			},
			wantErr: false,
		},
		{
			name: "missing token",
			cfg: Config{
				Telegram: TelegramConfig{BotToken: ""},
				Database: DatabaseConfig{Path: "path"},
			},
			wantErr: true,
		},
		{
			name: "duplicate item id",
			cfg: Config{
				Telegram: TelegramConfig{BotToken: "token"},
				Database: DatabaseConfig{Path: "path"},
				Items: []models.Item{
					{ID: 1, Name: "Item 1"},
					{ID: 1, Name: "Item 2"},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestApplyDefaults(t *testing.T) {
	cfg := &Config{}
	cfg.applyDefaults()

	expectedReminder := "09:00"
	if cfg.Bot.ReminderTime != expectedReminder {
		t.Errorf("expected default reminder time %s, got %s", expectedReminder, cfg.Bot.ReminderTime)
	}
	if cfg.Bot.PaginationSize != models.DefaultPaginationSize {
		t.Errorf("expected default pagination size %d, got %d", models.DefaultPaginationSize, cfg.Bot.PaginationSize)
	}
	if cfg.API.GRPC.Port != 8081 {
		t.Errorf("expected default gRPC port 8081, got %d", cfg.API.GRPC.Port)
	}
	if cfg.Bot.RateLimitMessages != models.RateLimitMessages {
		t.Errorf("expected default rate limit messages %d, got %d", models.RateLimitMessages, cfg.Bot.RateLimitMessages)
	}
}

func TestValidateItems(t *testing.T) {
	tests := []struct {
		name    string
		items   []models.Item
		wantErr bool
	}{
		{
			name: "Valid items",
			items: []models.Item{
				{ID: 1, Name: "Item 1"},
				{ID: 2, Name: "Item 2"},
			},
			wantErr: false,
		},
		{
			name: "Duplicate ID",
			items: []models.Item{
				{ID: 1, Name: "Item 1"},
				{ID: 1, Name: "Item 2"},
			},
			wantErr: true,
		},
		{
			name: "ID 0",
			items: []models.Item{
				{ID: 0, Name: "Item 1"},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateItems(tt.items)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateItems() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
