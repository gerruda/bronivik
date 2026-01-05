package bot

// Stop stops receiving Telegram updates (best-effort).
func (b *Bot) Stop() {
	if b == nil || b.bot == nil {
		return
	}
	b.bot.StopReceivingUpdates()
}
