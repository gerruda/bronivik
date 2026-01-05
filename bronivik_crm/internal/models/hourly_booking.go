package models

import "time"

type HourlyBooking struct {
	ID          int64     `json:"id"`
	UserID      int64     `json:"user_id"`
	CabinetID   int64     `json:"cabinet_id"`
	ItemName    string    `json:"item_name"`
	ClientName  string    `json:"client_name"`
	ClientPhone string    `json:"client_phone"`
	StartTime   time.Time `json:"start_time"`
	EndTime     time.Time `json:"end_time"`
	Status      string    `json:"status"`
	Comment     string    `json:"comment"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
