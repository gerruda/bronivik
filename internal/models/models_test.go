package models

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestUserState_Helpers(t *testing.T) {
	now := time.Now()
	state := &UserState{
		TempData: map[string]interface{}{
			"int64":   int64(123),
			"int":     123,
			"float":   123.45,
			"string":  "hello",
			"time":    "2025-01-01T10:00:00Z",
			"time2":   "2025-01-01T10:00:00+03:00",
			"time_t":  now,
			"dates":   []interface{}{"2025-01-01T10:00:00Z", "2025-01-02T10:00:00+03:00", now, "invalid"},
			"dates_t": []time.Time{now},
		},
	}

	t.Run("NilTempData", func(t *testing.T) {
		nilState := &UserState{}
		assert.Equal(t, int64(0), nilState.GetInt64("any"))
		assert.Equal(t, "", nilState.GetString("any"))
		assert.True(t, nilState.GetTime("any").IsZero())
		assert.Nil(t, nilState.GetDates("any"))
	})

	t.Run("GetInt64", func(t *testing.T) {
		assert.Equal(t, int64(123), state.GetInt64("int64"))
		assert.Equal(t, int64(123), state.GetInt64("int"))
		assert.Equal(t, int64(123), state.GetInt64("float"))
		assert.Equal(t, int64(0), state.GetInt64("string"))
		assert.Equal(t, int64(0), state.GetInt64("missing"))
	})

	t.Run("GetString", func(t *testing.T) {
		assert.Equal(t, "hello", state.GetString("string"))
		assert.Equal(t, "", state.GetString("int"))
		assert.Equal(t, "", state.GetString("missing"))
	})

	t.Run("GetTime", func(t *testing.T) {
		tm := state.GetTime("time")
		assert.False(t, tm.IsZero())
		assert.Equal(t, 2025, tm.Year())

		tm2 := state.GetTime("time_t")
		assert.Equal(t, now.Unix(), tm2.Unix())

		tm3 := state.GetTime("time2")
		assert.False(t, tm3.IsZero())

		assert.True(t, state.GetTime("int").IsZero())
		assert.True(t, state.GetTime("string").IsZero())
		assert.True(t, state.GetTime("missing").IsZero())
	})

	t.Run("GetDates", func(t *testing.T) {
		dates := state.GetDates("dates")
		assert.Len(t, dates, 3) // 2 strings + 1 time.Time, 1 invalid string ignored

		datesT := state.GetDates("dates_t")
		assert.Len(t, datesT, 1)

		assert.Nil(t, state.GetDates("string"))
		assert.Nil(t, state.GetDates("missing"))
	})
}
