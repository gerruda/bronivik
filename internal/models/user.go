package models

import (
	"database/sql"
	"time"
)

type User struct {
	ID               uint         `gorm:"primaryKey;autoIncrement"`
	TelegramID       int64        `gorm:"uniqueIndex;not null"` // Уникальный ID Telegram
	Username         string       `gorm:"size:255"`             // Юзернейм Telegram
	FirstName        string       `gorm:"size:255;not null"`    // Имя пользователя
	LastName         string       `gorm:"size:255"`             // Фамилия пользователя
	Phone            string       `gorm:"size:20"`              // Телефонный номер
	IsManager        bool         `gorm:"default:false"`        // Флаг менеджера
	IsBlacklisted    bool         `gorm:"default:false"`        // Флаг черного списка
	LanguageCode     string       `gorm:"size:10"`              // Код языка
	LastActivity     time.Time    `gorm:"autoCreateTime"`       // Время последней активности
	CreatedAt        time.Time    `gorm:"autoCreateTime"`       // Время создания
	UpdatedAt        time.Time    `gorm:"autoUpdateTime"`       // Время обновления
	ConsentGiven     bool         `gorm:"default:false"`        // Согласие на обработку данных
	ConsentGivenAt   sql.NullTime // Когда было дано согласие
	ConsentRevoked   bool         `gorm:"default:false"` // Отозвано ли согласие
	ConsentRevokedAt sql.NullTime // Когда было отозвано
}
