package database

import (
	"context"
	"testing"

	"bronivik/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestItemCRUD(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()

	item := &models.Item{
		Name:          "Item 1",
		Description:   "Desc 1",
		TotalQuantity: 5,
		SortOrder:     10,
		IsActive:      true,
	}

	// Create
	err := db.CreateItem(ctx, item)
	require.NoError(t, err)

	// Get by Name
	found, err := db.GetItemByName(ctx, "Item 1")
	require.NoError(t, err)
	assert.Equal(t, item.Description, found.Description)
	assert.Equal(t, item.TotalQuantity, found.TotalQuantity)

	// Update
	found.TotalQuantity = 10
	err = db.UpdateItem(ctx, found)
	require.NoError(t, err)

	updated, _ := db.GetItemByName(ctx, "Item 1")
	assert.Equal(t, int64(10), updated.TotalQuantity)

	// Deactivate
	err = db.DeactivateItem(ctx, updated.ID)
	require.NoError(t, err)

	activeItems, err := db.GetActiveItems(ctx)
	require.NoError(t, err)
	assert.Len(t, activeItems, 0)
}

func TestItemReordering(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()

	db.CreateItem(ctx, &models.Item{Name: "A", SortOrder: 1, IsActive: true, TotalQuantity: 1})
	db.CreateItem(ctx, &models.Item{Name: "B", SortOrder: 2, IsActive: true, TotalQuantity: 1})

	items, _ := db.GetActiveItems(ctx)
	require.Len(t, items, 2)
	assert.Equal(t, "A", items[0].Name)

	// Reorder B to 0 (should come before A)
	err := db.ReorderItem(ctx, items[1].ID, 0)
	require.NoError(t, err)

	items, _ = db.GetActiveItems(ctx)
	assert.Equal(t, "B", items[0].Name)
}
