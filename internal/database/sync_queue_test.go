package database

import (
	"context"
	"testing"
	"time"

	"bronivik/internal/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSyncQueueCRUD(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()

	task := &models.SyncTask{
		TaskType:  "upsert",
		BookingID: 100,
		Payload:   `{"test": true}`,
		Status:    "pending",
	}

	// Create
	err := db.CreateSyncTask(ctx, task)
	require.NoError(t, err)

	// Get Pending
	tasks, err := db.GetPendingSyncTasks(ctx, 10)
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	assert.Equal(t, int64(100), tasks[0].BookingID)

	// Update Status
	err = db.UpdateSyncTaskStatus(ctx, tasks[0].ID, "completed", "", nil)
	require.NoError(t, err)

	tasks, _ = db.GetPendingSyncTasks(ctx, 10)
	assert.Len(t, tasks, 0)

	// Failed tasks
	errMsg := "some error"
	err1 := db.CreateSyncTask(ctx, &models.SyncTask{TaskType: "test", BookingID: 101, Status: "failed", LastError: &errMsg})
	require.NoError(t, err1)
	failed, err := db.GetFailedSyncTasks(ctx)
	require.NoError(t, err)
	assert.Len(t, failed, 1)
	assert.Equal(t, "some error", *failed[0].LastError)

	// Retry logic
	task2 := &models.SyncTask{TaskType: "retry_test", BookingID: 102, Status: "pending"}
	err2 := db.CreateSyncTask(ctx, task2)
	require.NoError(t, err2)

	nextRetry := time.Now().Add(time.Hour)
	err = db.UpdateSyncTaskStatus(ctx, task2.ID, "retry", "temporary error", &nextRetry)
	require.NoError(t, err)

	// Should not be returned by GetPendingSyncTasks because nextRetry is in the future
	tasks, _ = db.GetPendingSyncTasks(ctx, 10)
	for _, task := range tasks {
		if task.ID == task2.ID {
			assert.Fail(t, "task with future retry should not be pending")
		}
	}

	// Update to past retry
	pastRetry := time.Now().Add(-time.Hour)
	err = db.UpdateSyncTaskStatus(ctx, task2.ID, "retry", "temporary error", &pastRetry)
	require.NoError(t, err)
	tasks, _ = db.GetPendingSyncTasks(ctx, 10)
	found := false
	for _, task := range tasks {
		if task.ID == task2.ID {
			found = true
			assert.Equal(t, 2, task.RetryCount)
		}
	}
	assert.True(t, found)
}
