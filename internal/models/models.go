package models

import "time"

type UserState struct {
	UserID      int64
	CurrentStep string
	TempData    map[string]interface{}
}

func (s *UserState) GetInt64(key string) int64 {
	if s.TempData == nil {
		return 0
	}
	val, ok := s.TempData[key]
	if !ok {
		return 0
	}
	switch v := val.(type) {
	case int64:
		return v
	case float64:
		return int64(v)
	case int:
		return int64(v)
	default:
		return 0
	}
}

func (s *UserState) GetTime(key string) time.Time {
	if s.TempData == nil {
		return time.Time{}
	}
	val, ok := s.TempData[key]
	if !ok {
		return time.Time{}
	}
	switch v := val.(type) {
	case time.Time:
		return v
	case string:
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			t, err = time.Parse("2006-01-02T15:04:05Z07:00", v)
			if err != nil {
				return time.Time{}
			}
		}
		return t
	default:
		return time.Time{}
	}
}

func (s *UserState) GetString(key string) string {
	if s.TempData == nil {
		return ""
	}
	val, ok := s.TempData[key]
	if !ok {
		return ""
	}
	if str, ok := val.(string); ok {
		return str
	}
	return ""
}

func (s *UserState) GetDates(key string) []time.Time {
	if s.TempData == nil {
		return nil
	}
	val, ok := s.TempData[key]
	if !ok {
		return nil
	}
	switch v := val.(type) {
	case []time.Time:
		return v
	case []interface{}:
		var dates []time.Time
		for _, item := range v {
			switch val := item.(type) {
			case string:
				t, err := time.Parse(time.RFC3339, val)
				if err != nil {
					t, err = time.Parse("2006-01-02T15:04:05Z07:00", val)
				}
				if err == nil {
					dates = append(dates, t)
				}
			case time.Time:
				dates = append(dates, val)
			}
		}
		return dates
	default:
		return nil
	}
}

type Availability struct {
	Date      time.Time `json:"date"`
	ItemID    int64     `json:"item_id"`
	Booked    int64     `json:"booked"`
	Available int64     `json:"available"`
}
