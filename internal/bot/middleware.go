package bot

import (
	"context"
	"time"
)

func (b *Bot) withRecovery(handler func()) {
	defer func() {
		if r := recover(); r != nil {
			if b.metrics != nil {
				b.metrics.ErrorsTotal.Inc()
			}
			b.logger.Error().Interface("panic", r).Msg("Recovered from panic in update handler")
		}
	}()
	handler()
}

func (b *Bot) trackActivity(userID int64) {
	if userID == 0 {
		return
	}
	// Run in background to not block the main loop
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := b.userService.UpdateUserActivity(ctx, userID); err != nil {
			b.logger.Error().Err(err).Int64("user_id", userID).Msg("Failed to update user activity")
		}
	}()
}
