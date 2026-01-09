package api

import (
	"context"
	"strings"
	"time"

	availabilityv1 "bronivik/internal/api/gen/availability/v1"
	"bronivik/internal/database"
	"bronivik/internal/models"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type AvailabilityService struct {
	availabilityv1.UnimplementedAvailabilityServiceServer
	db          *database.DB
	itemsByName map[string]*models.Item
}

func NewAvailabilityService(db *database.DB) *AvailabilityService {
	items := db.GetItems()
	idx := make(map[string]*models.Item, len(items))
	for _, it := range items {
		idx[strings.ToLower(strings.TrimSpace(it.Name))] = it
	}

	return &AvailabilityService{
		db:          db,
		itemsByName: idx,
	}
}

func (s *AvailabilityService) GetAvailability(ctx context.Context, req *availabilityv1.GetAvailabilityRequest) (
	*availabilityv1.GetAvailabilityResponse, error) {
	itemName := strings.TrimSpace(req.GetItemName())
	if itemName == "" {
		return nil, status.Error(codes.InvalidArgument, "item_name is required")
	}

	dateStr := strings.TrimSpace(req.GetDate())
	if dateStr == "" {
		return nil, status.Error(codes.InvalidArgument, "date is required")
	}

	item, ok := s.itemsByName[strings.ToLower(itemName)]
	if !ok {
		return nil, status.Error(codes.NotFound, "item not found")
	}

	date, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid date format; expected YYYY-MM-DD")
	}

	booked, err := s.db.GetBookedCount(ctx, item.ID, date)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to get booked count")
	}

	total := item.TotalQuantity
	available := int64(booked) < total

	return &availabilityv1.GetAvailabilityResponse{
		ItemName:    item.Name,
		Date:        dateStr,
		Available:   available,
		BookedCount: int64(booked),
		Total:       total,
	}, nil
}

func (s *AvailabilityService) GetAvailabilityBulk(ctx context.Context, req *availabilityv1.GetAvailabilityBulkRequest) (
	*availabilityv1.GetAvailabilityBulkResponse, error) {
	items := req.GetItems()
	dates := req.GetDates()
	if len(items) == 0 {
		return nil, status.Error(codes.InvalidArgument, "items is required")
	}
	if len(dates) == 0 {
		return nil, status.Error(codes.InvalidArgument, "dates is required")
	}

	results := make([]*availabilityv1.Availability, 0, len(items)*len(dates))
	for _, rawItem := range items {
		itemName := strings.TrimSpace(rawItem)
		item, ok := s.itemsByName[strings.ToLower(itemName)]
		if !ok {
			// Skip unknown items rather than failing the whole request.
			continue
		}

		for _, dateStr := range dates {
			dateStr = strings.TrimSpace(dateStr)
			date, err := time.Parse("2006-01-02", dateStr)
			if err != nil {
				return nil, status.Errorf(codes.InvalidArgument, "invalid date format: %s", dateStr)
			}

			booked, err := s.db.GetBookedCount(ctx, item.ID, date)
			if err != nil {
				return nil, status.Error(codes.Internal, "failed to get booked count")
			}

			total := item.TotalQuantity
			results = append(results, &availabilityv1.Availability{
				ItemName:    item.Name,
				Date:        dateStr,
				Available:   int64(booked) < total,
				BookedCount: int64(booked),
				Total:       total,
			})
		}
	}

	return &availabilityv1.GetAvailabilityBulkResponse{Results: results}, nil
}

func (s *AvailabilityService) ListItems(
	ctx context.Context,
	_ *availabilityv1.ListItemsRequest,
) (*availabilityv1.ListItemsResponse, error) {
	items := s.db.GetItems()
	out := make([]*availabilityv1.Item, 0, len(items))
	for _, it := range items {
		out = append(out, &availabilityv1.Item{
			Id:            it.ID,
			Name:          it.Name,
			TotalQuantity: it.TotalQuantity,
		})
	}
	return &availabilityv1.ListItemsResponse{Items: out}, nil
}
