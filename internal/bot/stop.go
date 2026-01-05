package bot

// Stop stops receiving Telegram updates (best-effort).
func (b *Bot) Stop() {
	if b == nil || b.tgService == nil {
		return
	}
	b.tgService.StopReceivingUpdates()
}
