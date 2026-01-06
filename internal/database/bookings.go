package database

import (
	"context"
	"fmt"
	"time"

	"bronivik/internal/models"
)

func (db *DB) CheckAvailability(ctx context.Context, itemID int64, date time.Time) (bool, error) {
	bookedCount, err := db.GetBookedCount(ctx, itemID, date)
	if err != nil {
		return false, fmt.Errorf("failed to check availability: %w", err)
	}

	db.mu.RLock()
	item, ok := db.itemsCache[itemID]
	db.mu.RUnlock()
	if !ok {
		return false, fmt.Errorf("item not found in cache: %d", itemID)
	}

	return bookedCount < int(item.TotalQuantity), nil
}

func (db *DB) GetBookedCount(ctx context.Context, itemID int64, date time.Time) (int, error) {
	query := `SELECT COUNT(*) FROM bookings WHERE item_id = ? AND date = ? AND status NOT IN (?, ?)`
	var count int
	err := db.QueryRowContext(ctx, query, itemID, date.Format("2006-01-02"), models.StatusCanceled, "rejected").Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get booked count: %w", err)
	}
	return count, nil
}

func (db *DB) CreateBooking(ctx context.Context, booking *models.Booking) error {
	query := `INSERT INTO bookings (
				user_id, user_name, user_nickname, phone, item_id, item_name, 
				date, status, comment, created_at, updated_at, version
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	now := time.Now()
	result, err := db.ExecContext(ctx, query,
		booking.UserID,
		booking.UserName,
		booking.UserNickname,
		booking.Phone,
		booking.ItemID,
		booking.ItemName,
		booking.Date.Format("2006-01-02"),
		booking.Status,
		booking.Comment,
		now,
		now,
		1,
	)
	if err != nil {
		return fmt.Errorf("failed to create booking: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("failed to get last insert id: %w", err)
	}
	booking.ID = id
	booking.CreatedAt = now
	booking.UpdatedAt = now
	booking.Version = 1

	return nil
}

func (db *DB) CreateBookingWithLock(ctx context.Context, booking *models.Booking) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	// 1. Check availability inside transaction
	var bookedCount int
	queryCount := `SELECT COUNT(*) FROM bookings WHERE item_id = ? AND date = ? AND status NOT IN (?, ?)`
	err = tx.QueryRowContext(ctx, queryCount, booking.ItemID,
		booking.Date.Format("2006-01-02"), models.StatusCanceled, "rejected").Scan(&bookedCount)
	if err != nil {
		return fmt.Errorf("failed to check availability in tx: %w", err)
	}

	db.mu.RLock()
	item, ok := db.itemsCache[booking.ItemID]
	db.mu.RUnlock()
	if !ok {
		return fmt.Errorf("item not found in cache: %d", booking.ItemID)
	}

	if bookedCount >= int(item.TotalQuantity) {
		return ErrNotAvailable
	}

	// 2. Create booking
	queryInsert := `INSERT INTO bookings (
				user_id, user_name, user_nickname, phone, item_id, item_name, 
				date, status, comment, created_at, updated_at, version
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	now := time.Now()
	result, err := tx.ExecContext(ctx, queryInsert,
		booking.UserID,
		booking.UserName,
		booking.UserNickname,
		booking.Phone,
		booking.ItemID,
		booking.ItemName,
		booking.Date.Format("2006-01-02"),
		booking.Status,
		booking.Comment,
		now,
		now,
		1,
	)
	if err != nil {
		return fmt.Errorf("failed to insert booking in tx: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("failed to get last insert id in tx: %w", err)
	}
	booking.ID = id
	booking.CreatedAt = now
	booking.UpdatedAt = now
	booking.Version = 1

	return tx.Commit()
}

func (db *DB) UpdateBookingComment(ctx context.Context, bookingID int64, comment string) error {
	query := `UPDATE bookings SET comment = ?, updated_at = ? WHERE id = ?`
	_, err := db.ExecContext(ctx, query, comment, time.Now(), bookingID)
	return err
}

func (db *DB) GetBooking(ctx context.Context, id int64) (*models.Booking, error) {
	var booking models.Booking
	var dateStr string
	query := `SELECT id, user_id, user_name, user_nickname, phone, item_id, 
	                 item_name, date(date), status, comment, created_at, 
					 updated_at, version 
              FROM bookings WHERE id = ?`
	err := db.QueryRowContext(ctx, query, id).Scan(
		&booking.ID, &booking.UserID, &booking.UserName, &booking.UserNickname, &booking.Phone,
		&booking.ItemID, &booking.ItemName, &dateStr, &booking.Status, &booking.Comment,
		&booking.CreatedAt, &booking.UpdatedAt, &booking.Version,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get booking: %w", err)
	}

	booking.Date, err = time.Parse("2006-01-02", dateStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse booking date %s: %w", dateStr, err)
	}
	return &booking, nil
}

func (db *DB) UpdateBookingStatus(ctx context.Context, id int64, status string) error {
	query := `UPDATE bookings SET status = ?, updated_at = ? WHERE id = ?`
	_, err := db.ExecContext(ctx, query, status, time.Now(), id)
	return err
}

func (db *DB) UpdateBookingStatusWithVersion(ctx context.Context, id, fromVersion int64, status string) error {
	query := `UPDATE bookings SET status = ?, version = version + 1, updated_at = ? WHERE id = ? AND version = ?`
	result, err := db.ExecContext(ctx, query, status, time.Now(), id, fromVersion)
	if err != nil {
		return fmt.Errorf("failed to update booking status: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrConcurrentModification
	}
	return nil
}

func (db *DB) GetBookingsByDateRange(ctx context.Context, startDate, endDate time.Time) ([]*models.Booking, error) {
	query := `SELECT id, user_id, user_name, user_nickname, phone, item_id, 
	                 item_name, date(date), status, comment, created_at, 
					 updated_at, version 
              FROM bookings WHERE date(date) >= ? AND date(date) <= ? ORDER BY date ASC`
	rows, err := db.QueryContext(ctx, query, startDate.Format("2006-01-02"), endDate.Format("2006-01-02"))
	if err != nil {
		return nil, fmt.Errorf("failed to get bookings by date range: %w", err)
	}
	defer rows.Close()

	var bookings []*models.Booking
	for rows.Next() {
		b := &models.Booking{}
		var dateStr string
		err := rows.Scan(
			&b.ID, &b.UserID, &b.UserName, &b.UserNickname, &b.Phone,
			&b.ItemID, &b.ItemName, &dateStr, &b.Status, &b.Comment,
			&b.CreatedAt, &b.UpdatedAt, &b.Version,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan booking: %w", err)
		}
		b.Date, _ = time.Parse("2006-01-02", dateStr)
		bookings = append(bookings, b)
	}
	return bookings, nil
}

func (db *DB) GetAvailabilityForPeriod(ctx context.Context, itemID int64, startDate time.Time, days int) ([]*models.Availability, error) {
	endDate := startDate.AddDate(0, 0, days-1)

	// Используем date() для нормализации даты в SQLite
	query := `SELECT date(date) as d, COUNT(*) as booked_count 
              FROM bookings 
              WHERE item_id = ? AND date BETWEEN ? AND ? AND status NOT IN (?, ?)
              GROUP BY d`

	rows, err := db.QueryContext(ctx, query, itemID,
		startDate.Format("2006-01-02"), endDate.Format("2006-01-02"),
		models.StatusCanceled, "rejected")
	if err != nil {
		return nil, fmt.Errorf("failed to get availability batch: %w", err)
	}
	defer rows.Close()

	bookedCounts := make(map[string]int)
	for rows.Next() {
		var dateStr string
		var count int
		if err := rows.Scan(&dateStr, &count); err != nil {
			return nil, err
		}
		bookedCounts[dateStr] = count
	}

	db.mu.RLock()
	item := db.itemsCache[itemID]
	db.mu.RUnlock()

	var availability []*models.Availability
	for i := 0; i < days; i++ {
		date := startDate.AddDate(0, 0, i)
		dateStr := date.Format("2006-01-02")
		booked := bookedCounts[dateStr]

		available := int(item.TotalQuantity) - booked
		if available < 0 {
			available = 0
		}

		availability = append(availability, &models.Availability{
			Date:      date,
			ItemID:    itemID,
			Booked:    int64(booked),
			Available: int64(available),
		})
	}
	return availability, nil
}

func (db *DB) UpdateBookingItem(ctx context.Context, id, itemID int64, itemName string) error {
	query := `UPDATE bookings SET item_id = ?, item_name = ?, updated_at = ? WHERE id = ?`
	_, err := db.ExecContext(ctx, query, itemID, itemName, time.Now(), id)
	return err
}

func (db *DB) UpdateBookingItemWithVersion(ctx context.Context, id, fromVersion, itemID int64, itemName string) error {
	query := `UPDATE bookings SET item_id = ?, item_name = ?, version = version + 1, updated_at = ? WHERE id = ? AND version = ?`
	result, err := db.ExecContext(ctx, query, itemID, itemName, time.Now(), id, fromVersion)
	if err != nil {
		return fmt.Errorf("failed to update booking item: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrConcurrentModification
	}
	return nil
}

func (db *DB) UpdateBookingItemAndStatusWithVersion(ctx context.Context, id, fromVersion, itemID int64, itemName, status string) error {
	query := `UPDATE bookings SET item_id = ?, item_name = ?, status = ?, version = version + 1, updated_at = ? WHERE id = ? AND version = ?`
	result, err := db.ExecContext(ctx, query, itemID, itemName, status, time.Now(), id, fromVersion)
	if err != nil {
		return fmt.Errorf("failed to update booking item and status: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrConcurrentModification
	}
	return nil
}

func (db *DB) GetUserBookings(ctx context.Context, userID int64) ([]*models.Booking, error) {
	// Get bookings for the last 2 weeks and future ones
	twoWeeksAgo := time.Now().AddDate(0, 0, -14).Format("2006-01-02")
	query := `SELECT id, user_id, user_name, user_nickname, phone, item_id, 
	                 item_name, date(date), status, comment, created_at, 
					 updated_at, version 
              FROM bookings WHERE user_id = ? AND date >= ? ORDER BY date DESC`
	rows, err := db.QueryContext(ctx, query, userID, twoWeeksAgo)
	if err != nil {
		return nil, fmt.Errorf("failed to get user bookings: %w", err)
	}
	defer rows.Close()

	var bookings []*models.Booking
	for rows.Next() {
		b := &models.Booking{}
		var dateStr string
		err := rows.Scan(
			&b.ID, &b.UserID, &b.UserName, &b.UserNickname, &b.Phone,
			&b.ItemID, &b.ItemName, &dateStr, &b.Status, &b.Comment,
			&b.CreatedAt, &b.UpdatedAt, &b.Version,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan booking: %w", err)
		}
		b.Date, err = time.Parse("2006-01-02", dateStr)
		if err != nil {
			return nil, fmt.Errorf("failed to parse booking date %s: %w", dateStr, err)
		}
		bookings = append(bookings, b)
	}
	return bookings, nil
}

func (db *DB) GetBookingWithAvailability(ctx context.Context, bookingID, newItemID int64) (*models.Booking, bool, error) {
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

func (db *DB) GetDailyBookings(ctx context.Context, startDate, endDate time.Time) (map[string][]*models.Booking, error) {
	bookings, err := db.GetBookingsByDateRange(ctx, startDate, endDate)
	if err != nil {
		return nil, err
	}

	daily := make(map[string][]*models.Booking)
	for _, b := range bookings {
		dateKey := b.Date.Format("2006-01-02")
		daily[dateKey] = append(daily[dateKey], b)
	}
	return daily, nil
}
