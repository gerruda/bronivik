package models

import "time"

type User struct {
	ID            int64     `json:"id"`
	TelegramID    int64     `json:"telegram_id"`
	Username      string    `json:"username"`
	FirstName     string    `json:"first_name"`
	LastName      string    `json:"last_name"`
	Phone         string    `json:"phone"`
	IsManager     bool      `json:"is_manager"`
	IsBlacklisted bool      `json:"is_blacklisted"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}
