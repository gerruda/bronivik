package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"bronivik/bronivik_crm/internal/api"
	"bronivik/bronivik_crm/internal/models"

	_ "github.com/mattn/go-sqlite3"
)

var (
	ErrSlotNotAvailable = errors.New("slot not available")
	ErrItemNotAvailable = errors.New("item not available")
	ErrBookingNotFound  = errors.New("booking not found")
	ErrBookingForbidden = errors.New("booking not owned by user")
	ErrBookingTooLate   = errors.New("booking already started")
	ErrBookingFinalized = errors.New("booking already finalized")
	ErrSlotMisaligned   = errors.New("slot not aligned with schedule")
)

// DB wraps sql.DB for the CRM bot.
type DB struct {
	*sql.DB
}

// --- Users CRUD ---

// GetOrCreateUserByTelegramID ensures a user row exists for given telegram id and stores basic profile fields.
// Phone can be empty; if provided, it will overwrite stored value.
func (db *DB) GetOrCreateUserByTelegramID(
	ctx context.Context,
	telegramID int64,
	username, firstName, lastName, phone string,
) (*models.User, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	u, err := getUserByTelegramIDTx(ctx, tx, telegramID)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}

	now := time.Now()
	if err == sql.ErrNoRows {
		query := `INSERT INTO users (
			telegram_id, username, first_name, last_name, phone, 
			is_manager, is_blacklisted, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, 0, 0, ?, ?)`
		res, err := tx.ExecContext(ctx, query, telegramID, username, firstName, lastName, phone, now, now)
		if err != nil {
			return nil, err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return nil, err
		}
		u = &models.User{
			ID: id, TelegramID: telegramID, Username: username,
			FirstName: firstName, LastName: lastName, CreatedAt: now, UpdatedAt: now,
		}
	} else {
		// best-effort update of profile fields; phone only if provided
		if phone != "" {
			_, _ = tx.ExecContext(ctx, `
					UPDATE users SET username = ?, first_name = ?, last_name = ?, phone = ?, updated_at = ? 
					WHERE id = ?`, username, firstName, lastName, phone, now, u.ID)
			u.Phone = phone
		} else {
			_, _ = tx.ExecContext(ctx, `
					UPDATE users SET username = ?, first_name = ?, last_name = ?, updated_at = ? 
					WHERE id = ?`, username, firstName, lastName, now, u.ID)
		}
		u.Username = username
		u.FirstName = firstName
		u.LastName = lastName
		u.UpdatedAt = now
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return u, nil
}

func getUserByTelegramIDTx(ctx context.Context, tx *sql.Tx, telegramID int64) (*models.User, error) {
	row := tx.QueryRowContext(ctx, `
		SELECT id, telegram_id, username, first_name, last_name, 
		       phone, is_manager, is_blacklisted, created_at, updated_at
		FROM users WHERE telegram_id = ? LIMIT 1`, telegramID)
	return scanUser(row)
}

// NewDB opens database at path and runs migrations.
func NewDB(path string) (*DB, error) {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}
	if err := createTables(db); err != nil {
		return nil, err
	}
	return &DB{db}, nil
}

func createTables(db *sql.DB) error {
	queries := []string{
		// Users (simplified; extend as needed)
		`CREATE TABLE IF NOT EXISTS users (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            telegram_id INTEGER UNIQUE NOT NULL,
            username TEXT,
            first_name TEXT,
            last_name TEXT,
            phone TEXT,
            is_manager BOOLEAN NOT NULL DEFAULT 0,
            is_blacklisted BOOLEAN NOT NULL DEFAULT 0,
            created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
            updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
        )`,

		// Cabinets
		`CREATE TABLE IF NOT EXISTS cabinets (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            name TEXT UNIQUE NOT NULL,
            description TEXT,
            is_active BOOLEAN DEFAULT 1,
            created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
            updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
        )`,

		// Cabinet schedules
		`CREATE TABLE IF NOT EXISTS cabinet_schedules (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            cabinet_id INTEGER NOT NULL,
            day_of_week INTEGER NOT NULL,
            start_time TEXT NOT NULL,
            end_time TEXT NOT NULL,
            slot_duration INTEGER DEFAULT 60,
            is_active BOOLEAN DEFAULT 1,
            created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
            updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
            FOREIGN KEY (cabinet_id) REFERENCES cabinets(id)
        )`,

		// Schedule overrides
		`CREATE TABLE IF NOT EXISTS cabinet_schedule_overrides (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            cabinet_id INTEGER NOT NULL,
            date DATETIME NOT NULL,
            is_closed BOOLEAN DEFAULT 0,
            start_time TEXT,
            end_time TEXT,
            created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
            updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
            FOREIGN KEY (cabinet_id) REFERENCES cabinets(id)
        )`,

		// Hourly bookings
		`CREATE TABLE IF NOT EXISTS hourly_bookings (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            user_id INTEGER NOT NULL,
            cabinet_id INTEGER NOT NULL,
			item_name TEXT,
			client_name TEXT,
			client_phone TEXT,
            start_time DATETIME NOT NULL,
            end_time DATETIME NOT NULL,
            status TEXT NOT NULL DEFAULT 'pending',
            comment TEXT,
            created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
            updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
            FOREIGN KEY (cabinet_id) REFERENCES cabinets(id),
            FOREIGN KEY (user_id) REFERENCES users(id)
        )`,

		// Indexes
		`CREATE INDEX IF NOT EXISTS idx_cabinets_active ON cabinets(is_active)`,
		`CREATE INDEX IF NOT EXISTS idx_schedules_cabinet ON cabinet_schedules(cabinet_id, day_of_week)`,
		`CREATE INDEX IF NOT EXISTS idx_overrides_cabinet_date ON cabinet_schedule_overrides(cabinet_id, date)`,
		`CREATE INDEX IF NOT EXISTS idx_hourly_bookings_times ON hourly_bookings(cabinet_id, start_time, end_time)`,
		`CREATE INDEX IF NOT EXISTS idx_hourly_bookings_status ON hourly_bookings(status)`,
	}

	for _, q := range queries {
		if _, err := db.Exec(q); err != nil {
			return fmt.Errorf("exec migration %s: %w", trimSQL(q), err)
		}
	}

	if err := ensureHourlyBookingColumns(db); err != nil {
		return err
	}
	return nil
}

func ensureHourlyBookingColumns(db *sql.DB) error {
	cols, err := tableColumns(db, "hourly_bookings")
	if err != nil {
		return err
	}
	toAdd := []struct {
		name     string
		typeDecl string
	}{
		{name: "item_name", typeDecl: "TEXT"},
		{name: "client_name", typeDecl: "TEXT"},
		{name: "client_phone", typeDecl: "TEXT"},
	}

	for _, c := range toAdd {
		if cols[c.name] {
			continue
		}
		if _, err := db.Exec(fmt.Sprintf("ALTER TABLE hourly_bookings ADD COLUMN %s %s", c.name, c.typeDecl)); err != nil {
			// SQLite returns error if column already exists (race / old db)
			if strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
				continue
			}
			return fmt.Errorf("add column %s: %w", c.name, err)
		}
	}
	return nil
}

func tableColumns(db *sql.DB, table string) (map[string]bool, error) {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols := make(map[string]bool)
	for rows.Next() {
		var (
			cid      int
			name     string
			typeDecl string
			notnull  int
			dflt     sql.NullString
			pk       int
		)
		if err := rows.Scan(&cid, &name, &typeDecl, &notnull, &dflt, &pk); err != nil {
			return nil, err
		}
		cols[name] = true
	}
	return cols, rows.Err()
}

// --- Cabinets CRUD ---

// CreateCabinet inserts a new cabinet.
func (db *DB) CreateCabinet(ctx context.Context, c *models.Cabinet) error {
	if c == nil {
		return fmt.Errorf("cabinet is nil")
	}
	now := time.Now()
	res, err := db.ExecContext(ctx, `INSERT INTO cabinets (name, description, is_active, created_at, updated_at)
        VALUES (?, ?, 1, ?, ?)`, c.Name, c.Description, now, now)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	c.ID = id
	c.IsActive = true
	c.CreatedAt = now
	c.UpdatedAt = now
	return nil
}

// ListActiveCabinets returns active cabinets sorted by id.
func (db *DB) ListActiveCabinets(ctx context.Context) ([]models.Cabinet, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, name, description, is_active, created_at, updated_at 
		FROM cabinets WHERE is_active = 1 ORDER BY id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var res []models.Cabinet
	for rows.Next() {
		cab, err := scanCabinet(rows)
		if err != nil {
			return nil, err
		}
		res = append(res, *cab)
	}
	return res, rows.Err()
}

// GetCabinet fetches a cabinet by id.
func (db *DB) GetCabinet(ctx context.Context, id int64) (*models.Cabinet, error) {
	row := db.QueryRowContext(ctx, `SELECT id, name, description, is_active, created_at, updated_at FROM cabinets WHERE id = ?`, id)
	return scanCabinet(row)
}

// UpdateCabinet updates name/description/active flag.
func (db *DB) UpdateCabinet(ctx context.Context, c *models.Cabinet) error {
	if c == nil {
		return fmt.Errorf("cabinet is nil")
	}
	_, err := db.ExecContext(ctx, `UPDATE cabinets SET name = ?, description = ?, is_active = ?, updated_at = ? WHERE id = ?`,
		c.Name, c.Description, c.IsActive, time.Now(), c.ID)
	return err
}

// DeactivateCabinet sets is_active to false.
func (db *DB) DeactivateCabinet(ctx context.Context, id int64) error {
	_, err := db.ExecContext(ctx, `UPDATE cabinets SET is_active = 0, updated_at = ? WHERE id = ?`, time.Now(), id)
	return err
}

// --- Cabinet schedules CRUD ---

// CreateSchedule inserts a new weekly schedule entry.
func (db *DB) CreateSchedule(ctx context.Context, s *models.CabinetSchedule) error {
	if s == nil {
		return fmt.Errorf("schedule is nil")
	}
	now := time.Now()
	query := `INSERT INTO cabinet_schedules (
		cabinet_id, day_of_week, start_time, end_time, 
		slot_duration, is_active, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, 1, ?, ?)`
	res, err := db.ExecContext(ctx, query, s.CabinetID, s.DayOfWeek, s.StartTime, s.EndTime, s.SlotDuration, now, now)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	s.ID = id
	s.IsActive = true
	s.CreatedAt = now
	s.UpdatedAt = now
	return nil
}

// --- Hourly bookings CRUD ---

// CreateHourlyBooking inserts a new hourly booking.
func (db *DB) CreateHourlyBooking(ctx context.Context, b *models.HourlyBooking) error {
	if b == nil {
		return fmt.Errorf("booking is nil")
	}
	now := time.Now()
	res, err := db.ExecContext(ctx, `
		INSERT INTO hourly_bookings (
			user_id, cabinet_id, item_name, client_name, client_phone, 
			start_time, end_time, status, comment, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		b.UserID, b.CabinetID, b.ItemName, b.ClientName, b.ClientPhone,
		b.StartTime, b.EndTime, b.Status, b.Comment, now, now)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	b.ID = id
	b.CreatedAt = now
	b.UpdatedAt = now
	return nil
}

// GetHourlyBooking returns booking by id.
func (db *DB) GetHourlyBooking(ctx context.Context, id int64) (*models.HourlyBooking, error) {
	row := db.QueryRowContext(ctx, `
		SELECT id, user_id, cabinet_id, item_name, client_name, client_phone, 
		       start_time, end_time, status, comment, created_at, updated_at 
		FROM hourly_bookings WHERE id = ?`, id)
	return scanHourly(row)
}

// ListHourlyBookingsByCabinet returns bookings for a cabinet within range.
func (db *DB) ListHourlyBookingsByCabinet(ctx context.Context, cabinetID int64, from, to time.Time) ([]models.HourlyBooking, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, user_id, cabinet_id, item_name, client_name, client_phone, 
		       start_time, end_time, status, comment, created_at, updated_at
        FROM hourly_bookings
        WHERE cabinet_id = ? AND start_time < ? AND end_time > ?
        ORDER BY start_time ASC`, cabinetID, to, from)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var res []models.HourlyBooking
	for rows.Next() {
		bk, err := scanHourly(rows)
		if err != nil {
			return nil, err
		}
		res = append(res, *bk)
	}
	return res, rows.Err()
}

// ListUserBookings returns up to limit bookings for a user; includePast controls whether past bookings are returned.
func (db *DB) ListUserBookings(ctx context.Context, userID int64, limit int, includePast bool) ([]models.HourlyBooking, error) {
	if limit <= 0 {
		limit = 10
	}
	args := []any{userID}
	query := `
		SELECT id, user_id, cabinet_id, item_name, client_name, client_phone, 
		       start_time, end_time, status, comment, created_at, updated_at
		FROM hourly_bookings WHERE user_id = ?`
	if !includePast {
		query += " AND end_time >= ?"
		args = append(args, time.Now())
	}
	query += " ORDER BY start_time ASC LIMIT ?"
	args = append(args, limit)

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var res []models.HourlyBooking
	for rows.Next() {
		bk, err := scanHourly(rows)
		if err != nil {
			return nil, err
		}
		res = append(res, *bk)
	}
	return res, rows.Err()
}

// UpdateHourlyBookingStatus updates status/comment and updated_at.
func (db *DB) UpdateHourlyBookingStatus(ctx context.Context, id int64, status, comment string) error {
	_, err := db.ExecContext(ctx, `
		UPDATE hourly_bookings SET status = ?, comment = ?, updated_at = ? 
		WHERE id = ?`, status, comment, time.Now(), id)
	return err
}

// DeleteHourlyBooking removes booking by id.
func (db *DB) DeleteHourlyBooking(ctx context.Context, id int64) error {
	_, err := db.ExecContext(ctx, `DELETE FROM hourly_bookings WHERE id = ?`, id)
	return err
}

// CancelUserBooking sets status to canceled if the booking belongs to the user and not started yet.
func (db *DB) CancelUserBooking(ctx context.Context, bookingID, userID int64) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var ownerID int64
	var status string
	var start time.Time
	if err := tx.QueryRowContext(ctx, `
		SELECT user_id, status, start_time 
		FROM hourly_bookings WHERE id = ?`, bookingID).Scan(&ownerID, &status, &start); err != nil {
		if err == sql.ErrNoRows {
			return ErrBookingNotFound
		}
		return err
	}
	if ownerID != userID {
		return ErrBookingForbidden
	}
	now := time.Now()
	if !start.After(now) {
		return ErrBookingTooLate
	}
	if status == "canceled" || status == "rejected" {
		return ErrBookingFinalized
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE hourly_bookings SET status = 'canceled', updated_at = ? 
		WHERE id = ?`, now, bookingID); err != nil {
		return err
	}
	return tx.Commit()
}

// CountActiveUserBookings returns count of future, non-canceled bookings for a user.
func (db *DB) CountActiveUserBookings(ctx context.Context, userID int64) (int, error) {
	row := db.QueryRowContext(ctx, `
		SELECT COUNT(1) FROM hourly_bookings 
		WHERE user_id = ? AND end_time >= ? 
		AND status NOT IN ('canceled','rejected')`, userID, time.Now())
	var count int
	if err := row.Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

// CheckSlotAvailability verifies if a time slot is free for a cabinet on a date.
func (db *DB) CheckSlotAvailability(ctx context.Context, cabinetID int64, date, start, end time.Time) (bool, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()

	ok, err := checkSlotAvailabilityTx(ctx, tx, cabinetID, date, start, end)
	if err != nil {
		return false, err
	}
	return ok, nil
}

// GetAvailableSlots returns all slots for the day based on schedule and bookings.
func (db *DB) GetAvailableSlots(ctx context.Context, cabinetID int64, date time.Time) ([]TimeSlot, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	startWin, endWin, slotDuration, err := resolveScheduleWindowTx(ctx, tx, cabinetID, date)
	if err != nil {
		return nil, err
	}
	if startWin.IsZero() || endWin.IsZero() {
		return nil, nil
	}

	if slotDuration <= 0 {
		slotDuration = 60
	}

	var slots []TimeSlot
	for cursor := startWin; cursor.Add(time.Duration(slotDuration)*time.Minute).Before(endWin) ||
		cursor.Add(time.Duration(slotDuration)*time.Minute).Equal(endWin); cursor = cursor.Add(time.Duration(slotDuration) * time.Minute) {
		s := cursor
		e := cursor.Add(time.Duration(slotDuration) * time.Minute)
		ok, err := checkSlotAvailabilityTx(ctx, tx, cabinetID, date, s, e)
		if err != nil {
			return nil, err
		}
		slots = append(slots, TimeSlot{
			StartTime: s.Format("15:04"),
			EndTime:   e.Format("15:04"),
			Available: ok,
		})
	}

	return slots, nil
}

// CreateHourlyBookingWithChecks checks slot and optional item availability before inserting.
func (db *DB) CreateHourlyBookingWithChecks(ctx context.Context, booking *models.HourlyBooking, client *api.BronivikClient) error {
	if booking == nil {
		return fmt.Errorf("booking is nil")
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if aErr := validateSlotAlignmentTx(ctx, tx, booking.CabinetID, booking.StartTime, booking.EndTime); aErr != nil {
		return aErr
	}

	// slot availability
	ok, err := checkSlotAvailabilityTx(ctx, tx, booking.CabinetID, booking.StartTime, booking.StartTime, booking.EndTime)
	if err != nil {
		return err
	}
	if !ok {
		return ErrSlotNotAvailable
	}

	// item availability via bronivik_jr API
	if client != nil && booking.ItemName != "" {
		dateStr := booking.StartTime.Format("2006-01-02")
		avail, aErr := client.GetAvailability(ctx, booking.ItemName, dateStr)
		if aErr != nil || avail == nil || !avail.Available {
			return ErrItemNotAvailable
		}
	}

	now := time.Now()
	res, err := tx.ExecContext(ctx, `
		INSERT INTO hourly_bookings (
			user_id, cabinet_id, item_name, client_name, client_phone, 
			start_time, end_time, status, comment, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		booking.UserID, booking.CabinetID, booking.ItemName, booking.ClientName,
		booking.ClientPhone, booking.StartTime, booking.EndTime, booking.Status,
		booking.Comment, now, now)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	booking.ID = id
	booking.CreatedAt = now
	booking.UpdatedAt = now

	return tx.Commit()
}

func checkSlotAvailabilityTx(ctx context.Context, tx *sql.Tx, cabinetID int64, date, start, end time.Time) (bool, error) {
	startWin, endWin, _, err := resolveScheduleWindowTx(ctx, tx, cabinetID, date)
	if err != nil {
		return false, err
	}
	if startWin.IsZero() || endWin.IsZero() {
		return false, nil
	}
	if start.Before(startWin) || end.After(endWin) || !end.After(start) {
		return false, nil
	}

	var count int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(1) FROM hourly_bookings
        WHERE cabinet_id = ? AND start_time < ? AND end_time > ? 
		AND status NOT IN ('canceled','rejected')`, cabinetID, end, start).Scan(&count); err != nil {
		return false, err
	}
	return count == 0, nil
}

func resolveScheduleWindowTx(
	ctx context.Context,
	tx *sql.Tx,
	cabinetID int64,
	date time.Time,
) (startWin, endWin time.Time, slotDuration int, err error) {
	day := int(date.Weekday())
	if day == 0 {
		day = 7 // make Monday=1..Sunday=7 consistent with UI
	}

	var sched models.CabinetSchedule
	row := tx.QueryRowContext(ctx, `SELECT id, cabinet_id, day_of_week, start_time, end_time, slot_duration, is_active, created_at, updated_at
        FROM cabinet_schedules WHERE cabinet_id = ? AND day_of_week = ? AND is_active = 1 LIMIT 1`, cabinetID, day)
	if err = row.Scan(
		&sched.ID, &sched.CabinetID, &sched.DayOfWeek, &sched.StartTime,
		&sched.EndTime, &sched.SlotDuration, &sched.IsActive, &sched.CreatedAt,
		&sched.UpdatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return time.Time{}, time.Time{}, 0, nil
		}
		return time.Time{}, time.Time{}, 0, err
	}

	var ovrStart, ovrEnd string
	var closed bool
	ovrStart, ovrEnd, closed, err = loadOverrideTx(ctx, tx, cabinetID, date)
	if err != nil {
		return time.Time{}, time.Time{}, 0, err
	}
	if closed {
		return time.Time{}, time.Time{}, 0, nil
	}

	startStr := sched.StartTime
	endStr := sched.EndTime
	if ovrStart != "" {
		startStr = ovrStart
	}
	if ovrEnd != "" {
		endStr = ovrEnd
	}

	startWin, err = combineDateTime(date, startStr)
	if err != nil {
		return time.Time{}, time.Time{}, 0, err
	}
	endWin, err = combineDateTime(date, endStr)
	if err != nil {
		return time.Time{}, time.Time{}, 0, err
	}

	return startWin, endWin, sched.SlotDuration, nil
}

// GetScheduleWindow returns schedule window and slot duration for a cabinet/date.
func (db *DB) GetScheduleWindow(
	ctx context.Context,
	cabinetID int64,
	date time.Time,
) (startWin, endWin time.Time, slotDuration int, err error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return time.Time{}, time.Time{}, 0, err
	}
	defer func() { _ = tx.Rollback() }()

	startWin, endWin, slotDuration, err = resolveScheduleWindowTx(ctx, tx, cabinetID, date)
	if err != nil {
		return time.Time{}, time.Time{}, 0, err
	}
	return startWin, endWin, slotDuration, nil
}

func validateSlotAlignmentTx(ctx context.Context, tx *sql.Tx, cabinetID int64, start, end time.Time) error {
	startWin, endWin, slotDuration, err := resolveScheduleWindowTx(ctx, tx, cabinetID, start)
	if err != nil {
		return err
	}
	if startWin.IsZero() || endWin.IsZero() || slotDuration <= 0 {
		return nil
	}

	slot := time.Duration(slotDuration) * time.Minute
	if !end.After(start) || end.Sub(start) != slot {
		return ErrSlotMisaligned
	}
	if start.Before(startWin) || end.After(endWin) {
		return ErrSlotMisaligned
	}
	delta := start.Sub(startWin)
	if delta%slot != 0 {
		return ErrSlotMisaligned
	}
	return nil
}

func loadOverrideTx(ctx context.Context, tx *sql.Tx, cabinetID int64, date time.Time) (start, end string, closed bool, err error) {
	row := tx.QueryRowContext(ctx, `
		SELECT start_time, end_time, is_closed 
		FROM cabinet_schedule_overrides 
		WHERE cabinet_id = ? AND date(date) = date(?) LIMIT 1`, cabinetID, date)
	if err = row.Scan(&start, &end, &closed); err != nil {
		if err == sql.ErrNoRows {
			return "", "", false, nil
		}
		return "", "", false, err
	}
	return
}

func combineDateTime(date time.Time, hm string) (time.Time, error) {
	parts := strings.Split(hm, ":")
	if len(parts) < 2 {
		return time.Time{}, fmt.Errorf("invalid time format: %s", hm)
	}
	hour, _ := strconv.Atoi(parts[0])
	minute, _ := strconv.Atoi(parts[1])
	return time.Date(date.Year(), date.Month(), date.Day(), hour, minute, 0, 0, date.Location()), nil
}

func scanHourly(r rowScanner) (*models.HourlyBooking, error) {
	var b models.HourlyBooking
	err := r.Scan(
		&b.ID, &b.UserID, &b.CabinetID, &b.ItemName, &b.ClientName,
		&b.ClientPhone, &b.StartTime, &b.EndTime, &b.Status, &b.Comment,
		&b.CreatedAt, &b.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// ListSchedulesByCabinet returns active schedules for a cabinet.
func (db *DB) ListSchedulesByCabinet(ctx context.Context, cabinetID int64) ([]models.CabinetSchedule, error) {
	query := `SELECT id, cabinet_id, day_of_week, start_time, end_time, 
		slot_duration, is_active, created_at, updated_at
		FROM cabinet_schedules WHERE cabinet_id = ? AND is_active = 1 
		ORDER BY day_of_week, id`
	rows, err := db.QueryContext(ctx, query, cabinetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var res []models.CabinetSchedule
	for rows.Next() {
		sched, err := scanSchedule(rows)
		if err != nil {
			return nil, err
		}
		res = append(res, *sched)
	}
	return res, rows.Err()
}

// UpdateSchedule updates schedule fields.
func (db *DB) UpdateSchedule(ctx context.Context, s *models.CabinetSchedule) error {
	if s == nil {
		return fmt.Errorf("schedule is nil")
	}
	_, err := db.ExecContext(ctx, `
		UPDATE cabinet_schedules 
		SET day_of_week = ?, start_time = ?, end_time = ?, 
		    slot_duration = ?, is_active = ?, updated_at = ? 
		WHERE id = ?`,
		s.DayOfWeek, s.StartTime, s.EndTime, s.SlotDuration, s.IsActive, time.Now(), s.ID)
	return err
}

// DeactivateSchedule turns off schedule entry.
func (db *DB) DeactivateSchedule(ctx context.Context, id int64) error {
	_, err := db.ExecContext(ctx, `UPDATE cabinet_schedules SET is_active = 0, updated_at = ? WHERE id = ?`, time.Now(), id)
	return err
}

// --- Helpers ---

type rowScanner interface {
	Scan(dest ...any) error
}

// TimeSlot represents a time window availability.
type TimeSlot struct {
	StartTime string
	EndTime   string
	Available bool
}

func scanCabinet(r rowScanner) (*models.Cabinet, error) {
	var c models.Cabinet
	if err := r.Scan(&c.ID, &c.Name, &c.Description, &c.IsActive, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return nil, err
	}
	return &c, nil
}

func scanUser(r rowScanner) (*models.User, error) {
	var u models.User
	if err := r.Scan(
		&u.ID, &u.TelegramID, &u.Username, &u.FirstName, &u.LastName,
		&u.Phone, &u.IsManager, &u.IsBlacklisted, &u.CreatedAt, &u.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return &u, nil
}

func scanSchedule(r rowScanner) (*models.CabinetSchedule, error) {
	var s models.CabinetSchedule
	if err := r.Scan(
		&s.ID, &s.CabinetID, &s.DayOfWeek, &s.StartTime, &s.EndTime,
		&s.SlotDuration, &s.IsActive, &s.CreatedAt, &s.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return &s, nil
}

func trimSQL(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 60 {
		return s[:60] + "..."
	}
	return s
}

// TouchUpdated sets updated_at for a row.
func TouchUpdated(db *sql.DB, table string, id int64) error {
	_, err := db.Exec(fmt.Sprintf("UPDATE %s SET updated_at = ? WHERE id = ?", table), time.Now(), id)
	return err
}
