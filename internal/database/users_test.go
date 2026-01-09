package database

import (
	"context"
	"testing"
	"time"

	"bronivik/internal/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserCRUD(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()

	user := &models.User{
		TelegramID:   12345,
		Username:     "testuser",
		FirstName:    "Test",
		LastName:     "User",
		Phone:        "79991234567",
		IsManager:    false,
		LastActivity: time.Now(),
	}

	// Create
	err := db.CreateOrUpdateUser(ctx, user)
	require.NoError(t, err)

	// Get by Telegram ID
	found, err := db.GetUserByTelegramID(ctx, 12345)
	require.NoError(t, err)
	assert.Equal(t, user.Username, found.Username)
	assert.Equal(t, user.FirstName, found.FirstName)
	assert.False(t, found.IsManager)

	// Update activity
	err = db.UpdateUserActivity(ctx, 12345)
	require.NoError(t, err)

	// Update phone
	err = db.UpdateUserPhone(ctx, 12345, "70000000000")
	require.NoError(t, err)

	found, _ = db.GetUserByTelegramID(ctx, 12345)
	assert.Equal(t, "70000000000", found.Phone)

	// Get all users
	users, err := db.GetAllUsers(ctx)
	require.NoError(t, err)
	assert.Len(t, users, 1)
}

func TestUserManagerStatus(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()

	user := &models.User{
		TelegramID:   111,
		FirstName:    "Manager",
		IsManager:    true,
		LastActivity: time.Now(),
	}
	err := db.CreateOrUpdateUser(ctx, user)
	require.NoError(t, err)

	user2 := &models.User{
		TelegramID:   222,
		FirstName:    "User",
		IsManager:    false,
		LastActivity: time.Now(),
	}
	err = db.CreateOrUpdateUser(ctx, user2)
	require.NoError(t, err)

	managers, err := db.GetUsersByManagerStatus(ctx, true)
	require.NoError(t, err)
	assert.Len(t, managers, 1)
	assert.Equal(t, int64(111), managers[0].TelegramID)

	nonManagers, err := db.GetUsersByManagerStatus(ctx, false)
	require.NoError(t, err)
	assert.Len(t, nonManagers, 1)
	assert.Equal(t, int64(222), nonManagers[0].TelegramID)
}

func TestUserBlacklist(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()

	user := &models.User{
		TelegramID:    333,
		FirstName:     "Bad User",
		IsBlacklisted: true,
		LastActivity:  time.Now(),
	}
	err := db.CreateOrUpdateUser(ctx, user)
	require.NoError(t, err)

	found, err := db.GetUserByTelegramID(ctx, 333)
	require.NoError(t, err)
	assert.True(t, found.IsBlacklisted)
}
func TestGetUserByID(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()

	user := &models.User{
		TelegramID:   444,
		FirstName:    "ID User",
		LastActivity: time.Now(),
	}
	err := db.CreateOrUpdateUser(ctx, user)
	require.NoError(t, err)

	// Get the auto-incremented ID
	foundByTG, _ := db.GetUserByTelegramID(ctx, 444)
	id := int64(foundByTG.ID)

	found, err := db.GetUserByID(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, int64(444), found.TelegramID)
}

func TestGetActiveUsers(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()

	// Active user
	err1 := db.CreateOrUpdateUser(ctx, &models.User{
		TelegramID:   555,
		FirstName:    "Active",
		LastActivity: time.Now(),
	})
	require.NoError(t, err1)

	// Inactive user
	err2 := db.CreateOrUpdateUser(ctx, &models.User{
		TelegramID:   666,
		FirstName:    "Inactive",
		LastActivity: time.Now().AddDate(0, 0, -40),
	})
	require.NoError(t, err2)

	active, err := db.GetActiveUsers(ctx, 30)
	require.NoError(t, err)
	assert.Len(t, active, 1)
	assert.Equal(t, int64(555), active[0].TelegramID)
}
