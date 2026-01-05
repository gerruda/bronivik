package database

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"bronivik/internal/models"
	_ "github.com/mattn/go-sqlite3"
)

type DB struct {
	*sql.DB
	items map[int64]models.Item
}

func NewDB(path string) (*DB, error) {
	// Создаем директорию для БД, если её нет
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %v", err)
	}

	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %v", err)
	}

	// Проверяем соединение
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to connect to database: %v", err)
	}

	// Создаем таблицы
	if err := createTables(db); err != nil {
		return nil, fmt.Errorf("failed to create tables: %v", err)
	}

	log.Printf("База данных инициализирована: %s", path)
	return &DB{db, make(map[int64]models.Item)}, nil
}

func createTables(db *sql.DB) error {
	queries := []string{
		// Таблица предметов (аппаратов)
		`CREATE TABLE IF NOT EXISTS items (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT UNIQUE NOT NULL,
			description TEXT,
			total_quantity INTEGER NOT NULL DEFAULT 1,
			sort_order INTEGER NOT NULL DEFAULT 0,
			is_active BOOLEAN NOT NULL DEFAULT 1,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		// Таблица пользователей
		`CREATE TABLE IF NOT EXISTS users (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            telegram_id INTEGER UNIQUE NOT NULL,
            username TEXT,
            first_name TEXT NOT NULL,
            last_name TEXT,
            phone TEXT,
            is_manager BOOLEAN NOT NULL DEFAULT 0,
            is_blacklisted BOOLEAN NOT NULL DEFAULT 0,
            language_code TEXT,
            last_activity DATETIME NOT NULL,
            created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
            updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
        )`,
		// Таблица бронирований
		`CREATE TABLE IF NOT EXISTS bookings (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            user_id INTEGER NOT NULL,
            user_name TEXT NOT NULL,
            user_nickname TEXT,
            phone TEXT NOT NULL,
            item_id INTEGER NOT NULL,
            item_name TEXT NOT NULL,
            date DATETIME NOT NULL,
            status TEXT NOT NULL DEFAULT 'pending',
            comment TEXT,
            created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
            updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
        )`,

		// Индексы для пользователей
		`CREATE INDEX IF NOT EXISTS idx_users_telegram_id ON users(telegram_id)`,
		`CREATE INDEX IF NOT EXISTS idx_users_is_manager ON users(is_manager)`,
		`CREATE INDEX IF NOT EXISTS idx_users_is_blacklisted ON users(is_blacklisted)`,

		// Индексы для items
		`CREATE INDEX IF NOT EXISTS idx_items_sort ON items(sort_order, id)`,

		// Существующие индексы для бронирований
		`CREATE INDEX IF NOT EXISTS idx_bookings_date ON bookings(date)`,
		`CREATE INDEX IF NOT EXISTS idx_bookings_status ON bookings(status)`,
		`CREATE INDEX IF NOT EXISTS idx_bookings_item_id ON bookings(item_id)`,
		`CREATE INDEX IF NOT EXISTS idx_bookings_user_id ON bookings(user_id)`,
	}

	for _, query := range queries {
		if _, err := db.Exec(query); err != nil {
			return fmt.Errorf("error executing query %s: %v", query, err)
		}
	}
	return nil
}

// SetItems устанавливает информацию о позициях для проверки доступности
func (db *DB) SetItems(items []models.Item) {
	db.items = make(map[int64]models.Item)
	for _, item := range items {
		db.items[item.ID] = item
	}
}

func (db *DB) itemByNameFromCache(name string) (*models.Item, bool) {
	lookup := strings.ToLower(strings.TrimSpace(name))
	for _, it := range db.items {
		if strings.ToLower(strings.TrimSpace(it.Name)) == lookup {
			itemCopy := it
			return &itemCopy, true
		}
	}
	return nil, false
}

// CreateItem вставляет новый item. Если SortOrder не задан, помещает в конец.
func (db *DB) CreateItem(ctx context.Context, item *models.Item) error {
	if item == nil {
		return fmt.Errorf("item is nil")
	}
	if item.SortOrder == 0 {
		var maxOrder sql.NullInt64
		if err := db.QueryRowContext(ctx, "SELECT COALESCE(MAX(sort_order),0) FROM items").Scan(&maxOrder); err != nil {
			return err
		}
		item.SortOrder = maxOrder.Int64 + 1
	}

	now := time.Now()
	res, err := db.ExecContext(ctx, `INSERT INTO items (name, description, total_quantity, sort_order, is_active, created_at, updated_at)
		VALUES (?, ?, ?, ?, 1, ?, ?)
	`, item.Name, item.Description, item.TotalQuantity, item.SortOrder, now, now)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	item.ID = id
	item.CreatedAt = now
	item.UpdatedAt = now
	item.IsActive = true
	return nil
}

// GetItemByName возвращает item по имени.
func (db *DB) GetItemByName(ctx context.Context, name string) (*models.Item, error) {
	row := db.QueryRowContext(ctx, `SELECT id, name, description, total_quantity, sort_order, is_active, created_at, updated_at
		FROM items WHERE name = ? LIMIT 1`, name)
	item, err := scanItem(row)
	if err == nil {
		return item, nil
	}
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}

	if cached, ok := db.itemByNameFromCache(name); ok {
		return cached, nil
	}

	return nil, sql.ErrNoRows
}

// GetItemAvailabilityByName returns availability info for the item on a given date.
func (db *DB) GetItemAvailabilityByName(ctx context.Context, itemName string, date time.Time) (*models.AvailabilityInfo, error) {
	item, err := db.GetItemByName(ctx, itemName)
	if err != nil {
		return nil, err
	}

	booked, err := db.GetBookedCount(ctx, item.ID, date)
	if err != nil {
		return nil, err
	}

	info := models.AvailabilityInfo{
		ItemName:    item.Name,
		Date:        date,
		BookedCount: int64(booked),
		Total:       item.TotalQuantity,
		Available:   int64(booked) < item.TotalQuantity,
	}

	return &info, nil
}

// GetActiveItems возвращает активные items, отсортированные по sort_order.
func (db *DB) GetActiveItems(ctx context.Context) ([]models.Item, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, name, description, total_quantity, sort_order, is_active, created_at, updated_at
		FROM items WHERE is_active = 1 ORDER BY sort_order ASC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []models.Item
	for rows.Next() {
		var it models.Item
		if err := rows.Scan(&it.ID, &it.Name, &it.Description, &it.TotalQuantity, &it.SortOrder, &it.IsActive, &it.CreatedAt, &it.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	return items, rows.Err()
}

// UpdateItem обновляет данные и, при необходимости, сортировку.
func (db *DB) UpdateItem(ctx context.Context, item *models.Item) error {
	if item == nil {
		return fmt.Errorf("item is nil")
	}
	current, err := db.GetItemByName(ctx, item.Name)
	if err != nil {
		return err
	}
	newOrder := item.SortOrder
	if newOrder <= 0 {
		newOrder = current.SortOrder
	}
	_, err = db.ExecContext(ctx, `UPDATE items SET description = ?, total_quantity = ?, sort_order = ?, updated_at = ? WHERE id = ?`,
		item.Description, item.TotalQuantity, newOrder, time.Now(), current.ID)
	return err
}

// DeactivateItem снимает item с публикации.
func (db *DB) DeactivateItem(ctx context.Context, id int64) error {
	_, err := db.ExecContext(ctx, `UPDATE items SET is_active = 0, updated_at = ? WHERE id = ?`, time.Now(), id)
	return err
}

// ReorderItem меняет sort_order и сдвигает соседей в транзакции.
func (db *DB) ReorderItem(ctx context.Context, id int64, newOrder int64) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var currentOrder int64
	if err := tx.QueryRowContext(ctx, `SELECT sort_order FROM items WHERE id = ?`, id).Scan(&currentOrder); err != nil {
		return err
	}
	if newOrder < 1 {
		newOrder = 1
	}
	var maxOrder int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(sort_order),1) FROM items`).Scan(&maxOrder); err != nil {
		return err
	}
	if newOrder > maxOrder {
		newOrder = maxOrder
	}

	if newOrder < currentOrder {
		// Сдвиг вниз (элемент поднимаем)
		if _, err := tx.ExecContext(ctx, `UPDATE items SET sort_order = sort_order + 1 WHERE sort_order >= ? AND sort_order < ?`, newOrder, currentOrder); err != nil {
			return err
		}
	} else if newOrder > currentOrder {
		// Сдвиг вверх (элемент опускаем)
		if _, err := tx.ExecContext(ctx, `UPDATE items SET sort_order = sort_order - 1 WHERE sort_order <= ? AND sort_order > ?`, newOrder, currentOrder); err != nil {
			return err
		}
	}

	if _, err := tx.ExecContext(ctx, `UPDATE items SET sort_order = ?, updated_at = ? WHERE id = ?`, newOrder, time.Now(), id); err != nil {
		return err
	}

	return tx.Commit()
}

func scanItem(row *sql.Row) (*models.Item, error) {
	var it models.Item
	if err := row.Scan(&it.ID, &it.Name, &it.Description, &it.TotalQuantity, &it.SortOrder, &it.IsActive, &it.CreatedAt, &it.UpdatedAt); err != nil {
		return nil, err
	}
	return &it, nil
}

// CheckAvailability проверяет доступность позиции на указанную дату
func (db *DB) CheckAvailability(ctx context.Context, itemID int64, date time.Time) (bool, error) {
	dateStr := date.Format("2006-01-02")

	query := `
        SELECT COUNT(*) 
        FROM bookings 
        WHERE item_id = ? 
        AND date(date) = date(?)
        AND status IN ('pending', 'confirmed')
    `

	var bookedCount int
	err := db.QueryRowContext(ctx, query, itemID, dateStr).Scan(&bookedCount)
	if err != nil {
		return false, err
	}

	// Получаем общее количество из кэша items
	item, exists := db.items[itemID]
	if !exists {
		return false, fmt.Errorf("item with ID %d not found", itemID)
	}

	return bookedCount < int(item.TotalQuantity), nil
}

// GetBookedCount возвращает количество забронированных единиц на дату
func (db *DB) GetBookedCount(ctx context.Context, itemID int64, date time.Time) (int, error) {
	dateStr := date.Format("2006-01-02")

	query := `
        SELECT COUNT(*) 
        FROM bookings 
        WHERE item_id = ? 
        AND date(date) = date(?)
        AND status IN ('pending', 'confirmed')
    `

	var count int
	err := db.QueryRowContext(ctx, query, itemID, dateStr).Scan(&count)
	return count, err
}

// CreateBooking создает новое бронирование
func (db *DB) CreateBooking(ctx context.Context, booking *models.Booking) error {
	query := `
        INSERT INTO bookings (user_id, user_name, user_nickname, phone, item_id, item_name, date, status, comment, created_at, updated_at)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
        RETURNING id
    `

	result, err := db.ExecContext(ctx, query,
		booking.UserID,
		booking.UserName,
		booking.UserNickname,
		booking.Phone,
		booking.ItemID,
		booking.ItemName,
		booking.Date,
		booking.Status,
		booking.Comment,
		booking.CreatedAt,
		booking.UpdatedAt,
	)

	if err != nil {
		return err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return err
	}

	booking.ID = id
	return nil
}

// UpdateBookingComment обновляет комментарий заявки
func (db *DB) UpdateBookingComment(ctx context.Context, bookingID int64, comment string) error {
	query := `UPDATE bookings SET comment = $1, updated_at = $2 WHERE id = $3`
	_, err := db.ExecContext(ctx, query, comment, time.Now(), bookingID)
	return err
}

// GetBooking возвращает бронирование по ID
func (db *DB) GetBooking(ctx context.Context, id int64) (*models.Booking, error) {
	query := `
        SELECT id, user_id, user_name, user_nickname, phone, item_id, item_name, date, status, comment, created_at, updated_at
        FROM bookings WHERE id = ?
    `

	var booking models.Booking
	err := db.QueryRowContext(ctx, query, id).Scan(
		&booking.ID,
		&booking.UserID,
		&booking.UserName,
		&booking.UserNickname,
		&booking.Phone,
		&booking.ItemID,
		&booking.ItemName,
		&booking.Date,
		&booking.Status,
		&booking.Comment,
		&booking.CreatedAt,
		&booking.UpdatedAt,
	)

	if err != nil {
		return nil, err
	}

	return &booking, nil
}

// UpdateBookingStatus обновляет статус бронирования
func (db *DB) UpdateBookingStatus(ctx context.Context, id int64, status string) error {
	query := `UPDATE bookings SET status = ? WHERE id = ?`

	_, err := db.ExecContext(ctx, query, status, id)
	return err
}

// GetBookingsByDateRange возвращает бронирования за период
func (db *DB) GetBookingsByDateRange(ctx context.Context, startDate, endDate time.Time) ([]models.Booking, error) {
	query := `
        SELECT id, user_id, user_name, user_nickname, phone, item_id, item_name, 
               date, status, comment, created_at, updated_at
        FROM bookings 
        WHERE strftime('%Y-%m-%d', date) BETWEEN ? AND ?
        ORDER BY date, created_at
    `

	rows, err := db.QueryContext(ctx, query,
		startDate.Format("2006-01-02"),
		endDate.Format("2006-01-02"))
	if err != nil {
		log.Printf("Ошибка в GetBookingsByDateRange: %v", err)
		return nil, err
	}
	defer rows.Close()

	var bookings []models.Booking
	count := 0
	for rows.Next() {
		var booking models.Booking
		err := rows.Scan(
			&booking.ID,
			&booking.UserID,
			&booking.UserName,
			&booking.UserNickname,
			&booking.Phone,
			&booking.ItemID,
			&booking.ItemName,
			&booking.Date,
			&booking.Status,
			&booking.Comment,
			&booking.CreatedAt,
			&booking.UpdatedAt,
		)
		if err != nil {
			log.Printf("Ошибка при сканировании строки %d: %v", count, err)
			return nil, err
		}
		bookings = append(bookings, booking)
		count++
	}

	log.Printf("Прочитано %d заявок", count)

	if err = rows.Err(); err != nil {
		log.Printf("Ошибка rows.Err(): %v", err)
		return nil, err
	}

	log.Printf("Возвращаем %d заявок", len(bookings))
	return bookings, nil
}

// GetAvailabilityForPeriod возвращает доступность на период
func (db *DB) GetAvailabilityForPeriod(ctx context.Context, itemID int64, startDate time.Time, days int) ([]models.Availability, error) {
	var availability []models.Availability

	item, exists := db.items[itemID]
	if !exists {
		return nil, fmt.Errorf("item with ID %d not found", itemID)
	}

	for i := 0; i < days; i++ {
		currentDate := startDate.AddDate(0, 0, i)
		booked, err := db.GetBookedCount(ctx, itemID, currentDate)
		if err != nil {
			return nil, err
		}

		availability = append(availability, models.Availability{
			Date:      currentDate,
			ItemID:    itemID,
			Booked:    int64(booked),
			Available: item.TotalQuantity - int64(booked),
		})
	}

	return availability, nil
}

// UpdateBookingItem обновляет данные о бронировании товара
func (db *DB) UpdateBookingItem(ctx context.Context, id int64, itemID int64, itemName string) error {
	query := `UPDATE bookings SET item_id = ?, item_name = ?, updated_at = ? WHERE id = ?`

	_, err := db.ExecContext(ctx, query, itemID, itemName, time.Now(), id)
	return err
}

// GetUserBookings возвращает список всех бронирований пользователя
func (db *DB) GetUserBookings(ctx context.Context, userID int64) ([]models.Booking, error) {
	// Рассчитываем дату 2 недели назад
	twoWeeksAgo := time.Now().AddDate(0, 0, -14)

	query := `
        SELECT id, user_id, user_name, user_nickname, phone, item_id, item_name, date, status, comment, created_at, updated_at
        FROM bookings 
        WHERE user_id = ? AND date >= ?
        ORDER BY created_at DESC
    `

	rows, err := db.QueryContext(ctx, query, userID, twoWeeksAgo.Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var bookings []models.Booking
	for rows.Next() {
		var booking models.Booking
		err := rows.Scan(
			&booking.ID,
			&booking.UserID,
			&booking.UserName,
			&booking.UserNickname,
			&booking.Phone,
			&booking.ItemID,
			&booking.ItemName,
			&booking.Date,
			&booking.Status,
			&booking.Comment,
			&booking.CreatedAt,
			&booking.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		bookings = append(bookings, booking)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return bookings, nil
}

// GetBookingWithAvailability проверяет доступность при смене аппарата
func (db *DB) GetBookingWithAvailability(ctx context.Context, bookingID int64, newItemID int64) (*models.Booking, bool, error) {
	booking, err := db.GetBooking(ctx, bookingID)
	if err != nil {
		return nil, false, err
	}

	available, err := db.CheckAvailability(ctx, newItemID, booking.Date)
	if err != nil {
		return nil, false, err
	}

	return booking, available, nil
}

// GetDailyBookings возвращает бронирования по дням для периода
func (db *DB) GetDailyBookings(ctx context.Context, startDate, endDate time.Time) (map[string][]models.Booking, error) {
	bookings, err := db.GetBookingsByDateRange(ctx, startDate, endDate)
	if err != nil {
		return nil, err
	}

	dailyBookings := make(map[string][]models.Booking)
	for _, booking := range bookings {
		dateKey := booking.Date.Format("2006-01-02")
		dailyBookings[dateKey] = append(dailyBookings[dateKey], booking)
	}

	return dailyBookings, nil
}

// GetItems возвращает список всех позиций
func (db *DB) GetItems() []models.Item {
	items := make([]models.Item, 0, len(db.items))
	for _, item := range db.items {
		items = append(items, item)
	}
	return items
}

// User methods

// CreateOrUpdateUser создает или обновляет пользователя
func (db *DB) CreateOrUpdateUser(ctx context.Context, user *models.User) error {
	query := `
        INSERT INTO users (telegram_id, username, first_name, last_name, phone, is_manager, is_blacklisted, language_code, last_activity, created_at, updated_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT(telegram_id) DO UPDATE SET
            username = excluded.username,
            first_name = excluded.first_name,
            last_name = excluded.last_name,
            phone = COALESCE(excluded.phone, phone),
            is_manager = excluded.is_manager,
            is_blacklisted = excluded.is_blacklisted,
            language_code = excluded.language_code,
            last_activity = excluded.last_activity,
            updated_at = excluded.updated_at
    `

	_, err := db.ExecContext(ctx, query,
		user.TelegramID,
		user.Username,
		user.FirstName,
		user.LastName,
		user.Phone,
		user.IsManager,
		user.IsBlacklisted,
		user.LanguageCode,
		user.LastActivity,
		user.CreatedAt,
		time.Now(),
	)

	return err
}

// GetUserByTelegramID возвращает пользователя по Telegram ID
func (db *DB) GetUserByTelegramID(ctx context.Context, telegramID int64) (*models.User, error) {
	query := `
        SELECT id, telegram_id, username, first_name, last_name, phone, is_manager, is_blacklisted, language_code, last_activity, created_at, updated_at
        FROM users WHERE telegram_id = ?
    `

	var user models.User
	err := db.QueryRowContext(ctx, query, telegramID).Scan(
		&user.ID,
		&user.TelegramID,
		&user.Username,
		&user.FirstName,
		&user.LastName,
		&user.Phone,
		&user.IsManager,
		&user.IsBlacklisted,
		&user.LanguageCode,
		&user.LastActivity,
		&user.CreatedAt,
		&user.UpdatedAt,
	)

	if err != nil {
		return nil, err
	}

	return &user, nil
}

// UpdateUserPhone обновляет номер телефона пользователя
func (db *DB) UpdateUserPhone(ctx context.Context, telegramID int64, phone string) error {
	query := `UPDATE users SET phone = ?, updated_at = ? WHERE telegram_id = ?`

	_, err := db.ExecContext(ctx, query, phone, time.Now(), telegramID)
	return err
}

// UpdateUserActivity обновляет время последней активности
func (db *DB) UpdateUserActivity(ctx context.Context, telegramID int64) error {
	query := `UPDATE users SET last_activity = ?, updated_at = ? WHERE telegram_id = ?`

	_, err := db.ExecContext(ctx, query, time.Now(), time.Now(), telegramID)
	return err
}

// GetAllUsers возвращает всех пользователей
func (db *DB) GetAllUsers(ctx context.Context) ([]models.User, error) {
	query := `
        SELECT id, telegram_id, username, first_name, last_name, phone, is_manager, is_blacklisted, language_code, last_activity, created_at, updated_at
        FROM users ORDER BY created_at DESC
    `

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []models.User
	for rows.Next() {
		var user models.User
		err := rows.Scan(
			&user.ID,
			&user.TelegramID,
			&user.Username,
			&user.FirstName,
			&user.LastName,
			&user.Phone,
			&user.IsManager,
			&user.IsBlacklisted,
			&user.LanguageCode,
			&user.LastActivity,
			&user.CreatedAt,
			&user.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		users = append(users, user)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return users, nil
}

// GetUsersByManagerStatus возвращает пользователей по статусу менеджера
func (db *DB) GetUsersByManagerStatus(ctx context.Context, isManager bool) ([]models.User, error) {
	query := `
        SELECT id, telegram_id, username, first_name, last_name, phone, is_manager, is_blacklisted, language_code, last_activity, created_at, updated_at
        FROM users WHERE is_manager = ? ORDER BY created_at DESC
    `

	rows, err := db.QueryContext(ctx, query, isManager)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []models.User
	for rows.Next() {
		var user models.User
		err := rows.Scan(
			&user.ID,
			&user.TelegramID,
			&user.Username,
			&user.FirstName,
			&user.LastName,
			&user.Phone,
			&user.IsManager,
			&user.IsBlacklisted,
			&user.LanguageCode,
			&user.LastActivity,
			&user.CreatedAt,
			&user.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		users = append(users, user)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return users, nil
}

// GetActiveUsers возвращает пользователей с активностью за последние N дней
func (db *DB) GetActiveUsers(ctx context.Context, days int) ([]models.User, error) {
	query := `
        SELECT id, telegram_id, username, first_name, last_name, phone, is_manager, is_blacklisted, language_code, last_activity, created_at, updated_at
        FROM users WHERE last_activity >= ? ORDER BY last_activity DESC
    `

	cutoffDate := time.Now().AddDate(0, 0, -days)
	rows, err := db.QueryContext(ctx, query, cutoffDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []models.User
	for rows.Next() {
		var user models.User
		err := rows.Scan(
			&user.ID,
			&user.TelegramID,
			&user.Username,
			&user.FirstName,
			&user.LastName,
			&user.Phone,
			&user.IsManager,
			&user.IsBlacklisted,
			&user.LanguageCode,
			&user.LastActivity,
			&user.CreatedAt,
			&user.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		users = append(users, user)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return users, nil
}

func (db *DB) Close() error {
	return db.DB.Close()
}
