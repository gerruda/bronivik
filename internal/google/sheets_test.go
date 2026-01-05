package google

import (
	"bronivik/internal/models"
	"testing"
	"time"
)

func TestFilterActiveBookings(t *testing.T) {
s := &SheetsService{}

bookings := []models.Booking{
{ID: 1, Status: "pending"},
{ID: 2, Status: "confirmed"},
{ID: 3, Status: "cancelled"},
{ID: 4, Status: "completed"},
}

active := s.filterActiveBookings(bookings)

if len(active) != 3 {
t.Errorf("Expected 3 active bookings, got %d", len(active))
}

for _, b := range active {
if b.Status == "cancelled" {
t.Errorf("Cancelled booking found in active list")
}
}
}

func TestBookingRowValues(t *testing.T) {
date := time.Date(2024, 12, 25, 0, 0, 0, 0, time.UTC)
createdAt := time.Date(2024, 12, 20, 10, 0, 0, 0, time.UTC)
updatedAt := time.Date(2024, 12, 21, 11, 0, 0, 0, time.UTC)

booking := &models.Booking{
ID:        123,
UserID:    456,
ItemID:    789,
Date:      date,
Status:    "confirmed",
UserName:  "Test User",
Phone:     "79991234567",
ItemName:  "Test Item",
CreatedAt: createdAt,
UpdatedAt: updatedAt,
}

values := bookingRowValues(booking)

expected := []interface{}{
int64(123),
int64(456),
int64(789),
"2024-12-25",
"confirmed",
"Test User",
"79991234567",
"Test Item",
"2024-12-20 10:00:00",
"2024-12-21 11:00:00",
}

if len(values) != len(expected) {
t.Fatalf("Expected %d values, got %d", len(expected), len(values))
}

for i, v := range values {
if v != expected[i] {
t.Errorf("At index %d: expected %v, got %v", i, expected[i], v)
}
}
}

func TestCacheOperations(t *testing.T) {
s := &SheetsService{
rowCache: make(map[int64]int),
}

s.setCachedRow(100, 5)
row, ok := s.getCachedRow(100)
if !ok || row != 5 {
t.Errorf("Expected row 5, got %d (ok=%v)", row, ok)
}

s.deleteCacheRow(100)
_, ok = s.getCachedRow(100)
if ok {
t.Errorf("Expected row to be deleted from cache")
}

s.setCachedRow(200, 10)
s.ClearCache()
_, ok = s.getCachedRow(200)
if ok {
t.Errorf("Expected cache to be cleared")
}
}
