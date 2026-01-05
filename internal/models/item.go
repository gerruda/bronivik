package models

import "time"

type Item struct {
	ID            int64     `yaml:"id"`
	Name          string    `yaml:"name"`
	Description   string    `yaml:"description"`
	TotalQuantity int64     `yaml:"total_quantity"`
	SortOrder     int64     `yaml:"sort_order" json:"sort_order"`
	IsActive      bool      `yaml:"is_active" json:"is_active"`
	CreatedAt     time.Time `yaml:"created_at" json:"created_at"`
	UpdatedAt     time.Time `yaml:"updated_at" json:"updated_at"`
}
