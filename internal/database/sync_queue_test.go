package database

import (
	"context"
	"testing"

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
	db.CreateSyncTask(ctx, &models.SyncTask{TaskType: "test", BookingID: 101, Status: "failed", LastError: &errMsg})
	failed, err := db.GetFailedSyncTasks(ctx)
	require.NoError(t, err)
	assert.Len(t, failed, 1)
	assert.Equal(t, "some error", *failed[0].LastError)
}
