package database

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"bronivik/bronivik_crm/internal/models"
)

func TestGetAvailableSlots_RespectsBookings(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "crm.db")
	db, err := NewDB(dbPath)
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	user, err := db.GetOrCreateUserByTelegramID(ctx, 123, "u", "First", "Last", "")
	if err != nil {
		t.Fatalf("GetOrCreateUserByTelegramID: %v", err)
	}

	cab := &models.Cabinet{Name: "Cab1", Description: ""}
	if err = db.CreateCabinet(ctx, cab); err != nil {
		t.Fatalf("CreateCabinet: %v", err)
	}

	date := time.Date(2026, 1, 5, 0, 0, 0, 0, time.Local)
	dow := int(date.Weekday())
	if dow == 0 {
		dow = 7
	}
	if err = db.CreateSchedule(ctx, &models.CabinetSchedule{
		CabinetID: cab.ID, DayOfWeek: dow, StartTime: "09:00",
		EndTime: "12:00", SlotDuration: 60,
	}); err != nil {
		t.Fatalf("CreateSchedule: %v", err)
	}

	bk := &models.HourlyBooking{
		UserID:    user.ID,
		CabinetID: cab.ID,
		StartTime: time.Date(2026, 1, 5, 10, 0, 0, 0, time.Local),
		EndTime:   time.Date(2026, 1, 5, 11, 0, 0, 0, time.Local),
		Status:    "pending",
	}
	if err = db.CreateHourlyBooking(ctx, bk); err != nil {
		t.Fatalf("CreateHourlyBooking: %v", err)
	}

	slots, err := db.GetAvailableSlots(ctx, cab.ID, date)
	if err != nil {
		t.Fatalf("GetAvailableSlots: %v", err)
	}
	if len(slots) != 3 {
		t.Fatalf("expected 3 slots, got %d", len(slots))
	}
	// 09-10 free, 10-11 busy, 11-12 free
	if slots[1].Available {
		t.Fatalf("expected middle slot to be unavailable")
	}
}

func TestCreateHourlyBookingWithChecks_BusySlot(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "crm.db")
	db, err := NewDB(dbPath)
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	user, err := db.GetOrCreateUserByTelegramID(ctx, 123, "u", "First", "Last", "")
	if err != nil {
		t.Fatalf("GetOrCreateUserByTelegramID: %v", err)
	}

	cab := &models.Cabinet{Name: "Cab1", Description: ""}
	if err = db.CreateCabinet(ctx, cab); err != nil {
		t.Fatalf("CreateCabinet: %v", err)
	}

	date := time.Date(2026, 1, 5, 0, 0, 0, 0, time.Local)
	dow := int(date.Weekday())
	if dow == 0 {
		dow = 7
	}
	err = db.CreateSchedule(ctx, &models.CabinetSchedule{
		CabinetID: cab.ID, DayOfWeek: dow, StartTime: "09:00",
		EndTime: "12:00", SlotDuration: 60,
	})
	if err != nil {
		t.Fatalf("CreateSchedule: %v", err)
	}

	busy := &models.HourlyBooking{
		UserID:    user.ID,
		CabinetID: cab.ID,
		StartTime: time.Date(2026, 1, 5, 10, 0, 0, 0, time.Local),
		EndTime:   time.Date(2026, 1, 5, 11, 0, 0, 0, time.Local),
		Status:    "pending",
	}
	if err = db.CreateHourlyBooking(ctx, busy); err != nil {
		t.Fatalf("CreateHourlyBooking: %v", err)
	}

	attempt := &models.HourlyBooking{
		UserID:    user.ID,
		CabinetID: cab.ID,
		StartTime: time.Date(2026, 1, 5, 10, 0, 0, 0, time.Local),
		EndTime:   time.Date(2026, 1, 5, 11, 0, 0, 0, time.Local),
		Status:    "pending",
	}
	if err := db.CreateHourlyBookingWithChecks(ctx, attempt, nil); err != ErrSlotNotAvailable {
		t.Fatalf("expected ErrSlotNotAvailable, got %v", err)
	}
}
