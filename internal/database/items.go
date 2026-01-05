package database

import (
	"context"
	"fmt"
	"time"

	"bronivik/internal/models"
)

func (db *DB) SetItems(items []models.Item) {
	db.items = make(map[int64]models.Item)
	for _, item := range items {
		db.items[item.ID] = item
	}
}

func (db *DB) itemByNameFromCache(name string) (*models.Item, bool) {
	for _, item := range db.items {
		if item.Name == name {
			return &item, true
		}
	}
	return nil, false
}

func (db *DB) CreateItem(ctx context.Context, item *models.Item) error {
	query := `INSERT INTO items (name, description, total_quantity, sort_order, is_active, created_at, updated_at)
              VALUES (?, ?, ?, ?, ?, ?, ?)`
	now := time.Now()
	result, err := db.ExecContext(ctx, query,
		item.Name,
		item.Description,
		item.TotalQuantity,
		item.SortOrder,
		item.IsActive,
		now,
		now,
	)
	if err != nil {
		return fmt.Errorf("failed to create item: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("failed to get last insert id: %w", err)
	}
	item.ID = id
	item.CreatedAt = now
	item.UpdatedAt = now

	// Update cache
	db.items[id] = *item

	return nil
}

func (db *DB) GetItemByName(ctx context.Context, name string) (*models.Item, error) {
	// Try cache first
	if item, ok := db.itemByNameFromCache(name); ok {
		return item, nil
	}

	var item models.Item
	query := `SELECT id, name, description, total_quantity, sort_order, is_active, created_at, updated_at FROM items WHERE name = ?`
	err := db.QueryRowContext(ctx, query, name).Scan(
		&item.ID, &item.Name, &item.Description, &item.TotalQuantity, &item.SortOrder, &item.IsActive, &item.CreatedAt, &item.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get item by name: %w", err)
	}
	return &item, nil
}

func (db *DB) GetItemAvailabilityByName(ctx context.Context, itemName string, date time.Time) (*models.AvailabilityInfo, error) {
	item, err := db.GetItemByName(ctx, itemName)
	if err != nil {
		return nil, err
	}

	bookedCount, err := db.GetBookedCount(ctx, item.ID, date)
	if err != nil {
		return nil, err
	}

	return &models.AvailabilityInfo{
		Available:   bookedCount < int(item.TotalQuantity),
		BookedCount: bookedCount,
		Total:       int(item.TotalQuantity),
	}, nil
}

func (db *DB) GetActiveItems(ctx context.Context) ([]models.Item, error) {
	query := `SELECT id, name, description, total_quantity, sort_order, is_active, created_at, updated_at 
              FROM items WHERE is_active = 1 ORDER BY sort_order, id`
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to get active items: %w", err)
	}
	defer rows.Close()

	var items []models.Item
	for rows.Next() {
		var item models.Item
		if err := rows.Scan(&item.ID, &item.Name, &item.Description, &item.TotalQuantity, &item.SortOrder, &item.IsActive, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan item: %w", err)
		}
		items = append(items, item)
	}
	return items, nil
}

func (db *DB) UpdateItem(ctx context.Context, item *models.Item) error {
	query := `UPDATE items SET name = ?, description = ?, total_quantity = ?, sort_order = ?, is_active = ?, updated_at = ? WHERE id = ?`
	now := time.Now()
	_, err := db.ExecContext(ctx, query, item.Name, item.Description, item.TotalQuantity, item.SortOrder, item.IsActive, now, item.ID)
	if err != nil {
		return fmt.Errorf("failed to update item: %w", err)
	}
	item.UpdatedAt = now
	db.items[item.ID] = *item
	return nil
}

func (db *DB) DeactivateItem(ctx context.Context, id int64) error {
	query := `UPDATE items SET is_active = 0, updated_at = ? WHERE id = ?`
	_, err := db.ExecContext(ctx, query, time.Now(), id)
	return err
}

func (db *DB) ReorderItem(ctx context.Context, id int64, newOrder int64) error {
	query := `UPDATE items SET sort_order = ?, updated_at = ? WHERE id = ?`
	_, err := db.ExecContext(ctx, query, newOrder, time.Now(), id)
	return err
}

func (db *DB) GetItems() []models.Item {
	var items []models.Item
	for _, item := range db.items {
		items = append(items, item)
	}
	return items
}
