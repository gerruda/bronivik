package models

import "time"

type Booking struct {
	ID           int64     `json:"id"`
	UserID       int64     `json:"user_id"`
	UserName     string    `json:"user_name"`
	UserNickname string    `json:"user_nickname"`
	Phone        string    `json:"phone"`
	ItemID       int64     `json:"item_id"`
	ItemName     string    `json:"item_name"`
	Date         time.Time `json:"date"`
	Status       string    `json:"status"` // pending, confirmed, canceled, changed, completed
	Comment      string    `json:"comment"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	Version      int64     `json:"version"`
}
