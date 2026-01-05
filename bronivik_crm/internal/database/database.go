package database

import (
    "context"
    "database/sql"
    "fmt"
    "strings"
    "time"

    "bronivik_crm/internal/models"
)

// DB wraps sql.DB for the CRM bot.
type DB struct {
    *sql.DB
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
    return nil
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
    rows, err := db.QueryContext(ctx, `SELECT id, name, description, is_active, created_at, updated_at FROM cabinets WHERE is_active = 1 ORDER BY id ASC`)
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
    res, err := db.ExecContext(ctx, `INSERT INTO cabinet_schedules (cabinet_id, day_of_week, start_time, end_time, slot_duration, is_active, created_at, updated_at)
        VALUES (?, ?, ?, ?, ?, 1, ?, ?)`, s.CabinetID, s.DayOfWeek, s.StartTime, s.EndTime, s.SlotDuration, now, now)
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
    res, err := db.ExecContext(ctx, `INSERT INTO hourly_bookings (user_id, cabinet_id, start_time, end_time, status, comment, created_at, updated_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, b.UserID, b.CabinetID, b.StartTime, b.EndTime, b.Status, b.Comment, now, now)
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
    row := db.QueryRowContext(ctx, `SELECT id, user_id, cabinet_id, start_time, end_time, status, comment, created_at, updated_at FROM hourly_bookings WHERE id = ?`, id)
    return scanHourly(row)
}

// ListHourlyBookingsByCabinet returns bookings for a cabinet within range.
func (db *DB) ListHourlyBookingsByCabinet(ctx context.Context, cabinetID int64, from, to time.Time) ([]models.HourlyBooking, error) {
    rows, err := db.QueryContext(ctx, `SELECT id, user_id, cabinet_id, start_time, end_time, status, comment, created_at, updated_at
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

// UpdateHourlyBookingStatus updates status/comment and updated_at.
func (db *DB) UpdateHourlyBookingStatus(ctx context.Context, id int64, status, comment string) error {
    _, err := db.ExecContext(ctx, `UPDATE hourly_bookings SET status = ?, comment = ?, updated_at = ? WHERE id = ?`, status, comment, time.Now(), id)
    return err
}

// DeleteHourlyBooking removes booking by id.
func (db *DB) DeleteHourlyBooking(ctx context.Context, id int64) error {
    _, err := db.ExecContext(ctx, `DELETE FROM hourly_bookings WHERE id = ?`, id)
    return err
}

func scanHourly(r rowScanner) (*models.HourlyBooking, error) {
    var b models.HourlyBooking
    if err := r.Scan(&b.ID, &b.UserID, &b.CabinetID, &b.StartTime, &b.EndTime, &b.Status, &b.Comment, &b.CreatedAt, &b.UpdatedAt); err != nil {
        return nil, err
    }
    return &b, nil
}

// ListSchedulesByCabinet returns active schedules for a cabinet.
func (db *DB) ListSchedulesByCabinet(ctx context.Context, cabinetID int64) ([]models.CabinetSchedule, error) {
    rows, err := db.QueryContext(ctx, `SELECT id, cabinet_id, day_of_week, start_time, end_time, slot_duration, is_active, created_at, updated_at
        FROM cabinet_schedules WHERE cabinet_id = ? AND is_active = 1 ORDER BY day_of_week, id`, cabinetID)
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
    _, err := db.ExecContext(ctx, `UPDATE cabinet_schedules SET day_of_week = ?, start_time = ?, end_time = ?, slot_duration = ?, is_active = ?, updated_at = ? WHERE id = ?`,
        s.DayOfWeek, s.StartTime, s.EndTime, s.SlotDuration, s.IsActive, time.Now(), s.ID)
    return err
}

// DeactivateSchedule turns off schedule entry.
func (db *DB) DeactivateSchedule(ctx context.Context, id int64) error {
    _, err := db.ExecContext(ctx, `UPDATE cabinet_schedules SET is_active = 0, updated_at = ? WHERE id = ?`, time.Now(), id)
    return err
}

// --- Helpers ---

type rowScanner interface{
    Scan(dest ...any) error
}

func scanCabinet(r rowScanner) (*models.Cabinet, error) {
    var c models.Cabinet
    if err := r.Scan(&c.ID, &c.Name, &c.Description, &c.IsActive, &c.CreatedAt, &c.UpdatedAt); err != nil {
        return nil, err
    }
    return &c, nil
}

func scanSchedule(r rowScanner) (*models.CabinetSchedule, error) {
    var s models.CabinetSchedule
    if err := r.Scan(&s.ID, &s.CabinetID, &s.DayOfWeek, &s.StartTime, &s.EndTime, &s.SlotDuration, &s.IsActive, &s.CreatedAt, &s.UpdatedAt); err != nil {
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
