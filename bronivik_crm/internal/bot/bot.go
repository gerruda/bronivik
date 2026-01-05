package bot

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	crmapi "bronivik/bronivik_crm/internal/api"
	"bronivik/bronivik_crm/internal/database"
	"bronivik/bronivik_crm/internal/models"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// Bot is a thin Telegram bot wrapper for CRM flow.
type Bot struct {
	api      *crmapi.BronivikClient
	db       *database.DB
	managers map[int64]struct{}
	bot      *tgbotapi.BotAPI
	state    *stateStore
	rules    BookingRules
}

type BookingRules struct {
	MinAdvance time.Duration
	MaxAdvance time.Duration
}

func New(token string, apiClient *crmapi.BronivikClient, db *database.DB, managers []int64, rules BookingRules) (*Bot, error) {
	b, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, err
	}
	mgrs := make(map[int64]struct{})
	for _, id := range managers {
		mgrs[id] = struct{}{}
	}
	if rules.MinAdvance <= 0 {
		rules.MinAdvance = 60 * time.Minute
	}
	if rules.MaxAdvance <= 0 {
		rules.MaxAdvance = 30 * 24 * time.Hour
	}
	return &Bot{api: apiClient, db: db, managers: mgrs, bot: b, state: newStateStore(), rules: rules}, nil
}

// Start begins polling updates and handles commands.
func (b *Bot) Start(ctx context.Context) {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := b.bot.GetUpdatesChan(u)
	log.Printf("CRM bot authorized as %s", b.bot.Self.UserName)

	for {
		select {
		case <-ctx.Done():
			return
		case update := <-updates:
			b.handleUpdate(ctx, update)
		}
	}
}

func (b *Bot) handleUpdate(ctx context.Context, update tgbotapi.Update) {
	if update.CallbackQuery != nil {
		b.handleCallback(ctx, update.CallbackQuery)
		return
	}
	if update.Message != nil {
		b.handleMessage(ctx, update.Message)
		return
	}
}

func (b *Bot) handleMessage(ctx context.Context, msg *tgbotapi.Message) {
	if msg == nil {
		return
	}
	text := strings.TrimSpace(msg.Text)
	st := b.state.get(msg.From.ID)

	switch {
	case strings.HasPrefix(text, "/start"):
		b.reply(msg.Chat.ID, "Добро пожаловать в бронь кабинетов! Используйте /book для создания заявки.")
	case strings.HasPrefix(text, "/help"):
		b.reply(msg.Chat.ID, "Доступные команды: /book, /my_bookings, /cancel_booking <id>, /help")
	case strings.HasPrefix(text, "/book"):
		b.startBookingFlow(ctx, msg)
	case strings.HasPrefix(text, "/my_bookings"):
		b.handleMyBookings(ctx, msg)
	case strings.HasPrefix(text, "/cancel_booking"):
		b.handleCancelBooking(ctx, msg)
	default:
		// flow text steps
		switch st.Step {
		case stepClientName:
			st.Draft.ClientName = text
			st.Step = stepClientPhone
			b.reply(msg.Chat.ID, "Введите телефон клиента:")
			return
		case stepClientPhone:
			phone, ok := normalizeAndValidatePhone(text)
			if !ok {
				b.reply(msg.Chat.ID, "Некорректный телефон. Пример: +7 999 123-45-67")
				return
			}
			st.Draft.ClientPhone = phone
			st.Step = stepConfirm
			b.sendConfirm(msg.Chat.ID, msg.From.ID)
			return
		}

		if b.isManager(msg.From.ID) {
			if b.handleManagerCommands(msg) {
				return
			}
		}
	}
}

func (b *Bot) handleCallback(ctx context.Context, cq *tgbotapi.CallbackQuery) {
	if cq == nil {
		return
	}
	data := cq.Data
	_ = b.answerCallback(cq.ID)
	if data == "noop" {
		return
	}

	userID := cq.From.ID
	chatID := cq.Message.Chat.ID
	st := b.state.get(userID)

	switch {
	case strings.HasPrefix(data, "cab:"):
		idStr := strings.TrimPrefix(data, "cab:")
		cabID, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			b.reply(chatID, "Некорректный кабинет")
			return
		}
		cab, err := b.db.GetCabinet(ctx, cabID)
		if err != nil {
			b.reply(chatID, "Не удалось загрузить кабинет")
			return
		}
		st.Draft.CabinetID = cabID
		st.Draft.CabinetName = cab.Name
		st.Step = stepItem
		b.sendItems(chatID)
		return

	case strings.HasPrefix(data, "item:"):
		name := strings.TrimPrefix(data, "item:")
		if name == "none" {
			name = ""
		}
		st.Draft.ItemName = name
		st.Step = stepDate
		b.sendCalendar(chatID, st.Draft.CabinetID)
		return

	case strings.HasPrefix(data, "date:"):
		dateStr := strings.TrimPrefix(data, "date:")
		st.Draft.Date = dateStr
		st.Step = stepTime
		b.sendTimeSlots(ctx, chatID, userID)
		return

	case strings.HasPrefix(data, "back:"):
		st.Step = stepDate
		b.sendCalendar(chatID, st.Draft.CabinetID)
		return

	case strings.HasPrefix(data, "slot:"):
		label := strings.TrimPrefix(data, "slot:")
		if st.Draft.Date == "" {
			b.reply(chatID, "Сначала выберите дату")
			return
		}
		date, err := time.Parse("2006-01-02", st.Draft.Date)
		if err != nil {
			b.reply(chatID, "Некорректная дата")
			return
		}
		start, end, err := parseTimeLabel(date, label)
		if err != nil {
			b.reply(chatID, "Некорректный слот")
			return
		}
		if err := b.validateBookingTime(start); err != nil {
			b.reply(chatID, err.Error())
			b.sendTimeSlots(ctx, chatID, userID)
			return
		}
		ok, err := b.db.CheckSlotAvailability(ctx, st.Draft.CabinetID, date, start, end)
		if err != nil {
			b.reply(chatID, "Не удалось проверить слот")
			return
		}
		if !ok {
			b.reply(chatID, "Слот занят. Выберите другой.")
			b.sendTimeSlots(ctx, chatID, userID)
			return
		}
		if b.api != nil && st.Draft.ItemName != "" {
			avail, err := b.api.GetAvailability(ctx, st.Draft.ItemName, st.Draft.Date)
			if err != nil || avail == nil || !avail.Available {
				b.reply(chatID, "Аппарат недоступен на эту дату. Выберите другой аппарат или 'Без аппарата'.")
				st.Step = stepItem
				b.sendItems(chatID)
				return
			}
		}
		st.Draft.TimeLabel = label
		st.Step = stepClientName
		b.reply(chatID, "Введите ФИО клиента:")
		return

	case data == "confirm":
		if st.Step != stepConfirm {
			b.reply(chatID, "Сценарий устарел, начните заново: /book")
			return
		}
		if err := b.finalizeBooking(ctx, cq, st); err != nil {
			if errors.Is(err, database.ErrSlotNotAvailable) {
				b.reply(chatID, "Слот уже занят. Выберите другое время.")
				st.Step = stepTime
				b.sendTimeSlots(ctx, chatID, userID)
				return
			}
			if errors.Is(err, database.ErrItemNotAvailable) {
				b.reply(chatID, "Аппарат недоступен на эту дату. Выберите другой аппарат или 'Без аппарата'.")
				st.Step = stepItem
				b.sendItems(chatID)
				return
			}
			b.reply(chatID, "Не удалось создать бронирование")
			return
		}
		b.state.reset(userID)
		return

	case data == "cancel":
		b.state.reset(userID)
		b.reply(chatID, "Ок, отменено. /book чтобы начать заново")
		return

	case strings.HasPrefix(data, "mgr:approve:"):
		if !b.isManager(userID) {
			return
		}
		idStr := strings.TrimPrefix(data, "mgr:approve:")
		bid, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			return
		}
		_ = b.db.UpdateHourlyBookingStatus(ctx, bid, "approved", "")
		b.reply(chatID, fmt.Sprintf("Бронирование #%d подтверждено", bid))
		b.notifyBookingStatus(ctx, bid, "approved")
		return

	case strings.HasPrefix(data, "mgr:reject:"):
		if !b.isManager(userID) {
			return
		}
		idStr := strings.TrimPrefix(data, "mgr:reject:")
		bid, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			return
		}
		_ = b.db.UpdateHourlyBookingStatus(ctx, bid, "rejected", "")
		b.reply(chatID, fmt.Sprintf("Бронирование #%d отклонено", bid))
		b.notifyBookingStatus(ctx, bid, "rejected")
		return
	}
}

func (b *Bot) validateBookingTime(start time.Time) error {
	now := time.Now()
	if start.Before(now.Add(b.rules.MinAdvance)) {
		min := int(b.rules.MinAdvance.Minutes())
		return fmt.Errorf("Слишком близко по времени. Минимум за %d минут до начала.", min)
	}
	if start.After(now.Add(b.rules.MaxAdvance)) {
		days := int(b.rules.MaxAdvance.Hours() / 24)
		if days <= 0 {
			days = 30
		}
		return fmt.Errorf("Слишком далеко по времени. Доступно бронирование максимум на %d дней вперед.", days)
	}
	return nil
}

func (b *Bot) handleManagerCommands(msg *tgbotapi.Message) bool {
	text := msg.Text
	switch {
	case strings.HasPrefix(text, "/add_cabinet"):
		b.reply(msg.Chat.ID, "(stub) Добавить кабинет")
	case strings.HasPrefix(text, "/list_cabinets"):
		b.reply(msg.Chat.ID, "(stub) Список кабинетов")
	case strings.HasPrefix(text, "/cabinet_schedule"):
		b.reply(msg.Chat.ID, "(stub) Расписание кабинета")
	case strings.HasPrefix(text, "/set_schedule"):
		b.reply(msg.Chat.ID, "(stub) Установить расписание")
	case strings.HasPrefix(text, "/close_cabinet"):
		b.reply(msg.Chat.ID, "(stub) Закрыть кабинет на дату")
	case strings.HasPrefix(text, "/pending"):
		b.reply(msg.Chat.ID, "(stub) Ожидающие подтверждения")
	case strings.HasPrefix(text, "/approve"):
		b.reply(msg.Chat.ID, "(stub) Подтвердить бронирование")
	case strings.HasPrefix(text, "/reject"):
		b.reply(msg.Chat.ID, "(stub) Отклонить бронирование")
	case strings.HasPrefix(text, "/today_schedule"):
		b.reply(msg.Chat.ID, "(stub) Расписание на сегодня")
	case strings.HasPrefix(text, "/tomorrow_schedule"):
		b.reply(msg.Chat.ID, "(stub) Расписание на завтра")
	default:
		return false
	}
	return true
}

func (b *Bot) handleMyBookings(ctx context.Context, msg *tgbotapi.Message) {
	if msg == nil || msg.From == nil {
		return
	}
	u, err := b.db.GetOrCreateUserByTelegramID(ctx, msg.From.ID, msg.From.UserName, msg.From.FirstName, msg.From.LastName)
	if err != nil {
		b.reply(msg.Chat.ID, "Не удалось загрузить пользователя")
		return
	}

	bookings, err := b.db.ListUserBookings(ctx, u.ID, 10, false)
	if err != nil {
		b.reply(msg.Chat.ID, "Не удалось получить бронирования")
		return
	}
	if len(bookings) == 0 {
		b.reply(msg.Chat.ID, "У вас нет активных бронирований")
		return
	}

	var sb strings.Builder
	sb.WriteString("Ваши бронирования:\n")
	for _, bk := range bookings {
		cabName := fmt.Sprintf("Кабинет #%d", bk.CabinetID)
		if cab, err := b.db.GetCabinet(ctx, bk.CabinetID); err == nil && cab != nil {
			cabName = cab.Name
		}
		item := bk.ItemName
		if item == "" {
			item = "Без аппарата"
		}
		line := fmt.Sprintf("#%d %s %s-%s | %s | %s | %s\n",
			bk.ID,
			bk.StartTime.Format("02.01"),
			bk.StartTime.Format("15:04"),
			bk.EndTime.Format("15:04"),
			cabName,
			item,
			bk.Status,
		)
		sb.WriteString(line)
	}
	b.reply(msg.Chat.ID, sb.String())
}

func (b *Bot) handleCancelBooking(ctx context.Context, msg *tgbotapi.Message) {
	if msg == nil || msg.From == nil {
		return
	}
	parts := strings.Fields(msg.Text)
	if len(parts) < 2 {
		b.reply(msg.Chat.ID, "Формат: /cancel_booking <id>")
		return
	}
	id, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || id <= 0 {
		b.reply(msg.Chat.ID, "Некорректный id бронирования")
		return
	}

	u, err := b.db.GetOrCreateUserByTelegramID(ctx, msg.From.ID, msg.From.UserName, msg.From.FirstName, msg.From.LastName)
	if err != nil {
		b.reply(msg.Chat.ID, "Не удалось загрузить пользователя")
		return
	}

	switch err := b.db.CancelUserBooking(ctx, id, u.ID); {
	case err == nil:
		b.reply(msg.Chat.ID, fmt.Sprintf("Бронирование #%d отменено", id))
	case errors.Is(err, database.ErrBookingNotFound):
		b.reply(msg.Chat.ID, "Бронирование не найдено")
	case errors.Is(err, database.ErrBookingForbidden):
		b.reply(msg.Chat.ID, "Нельзя отменить чужое бронирование")
	case errors.Is(err, database.ErrBookingTooLate):
		b.reply(msg.Chat.ID, "Нельзя отменить уже начавшееся бронирование")
	case errors.Is(err, database.ErrBookingFinalized):
		b.reply(msg.Chat.ID, "Бронирование уже завершено или отменено")
	default:
		b.reply(msg.Chat.ID, "Не удалось отменить бронирование")
	}
}

func (b *Bot) reply(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	_, _ = b.bot.Send(msg)
}

func (b *Bot) isManager(id int64) bool {
	_, ok := b.managers[id]
	return ok
}

func (b *Bot) answerCallback(id string) error {
	_, err := b.bot.Request(tgbotapi.NewCallback(id, ""))
	return err
}

func (b *Bot) startBookingFlow(ctx context.Context, msg *tgbotapi.Message) {
	if msg == nil {
		return
	}
	b.state.reset(msg.From.ID)
	st := b.state.get(msg.From.ID)
	st.Step = stepCabinet

	cabs, err := b.db.ListActiveCabinets(ctx)
	if err != nil || len(cabs) == 0 {
		b.reply(msg.Chat.ID, "Нет доступных кабинетов")
		return
	}
	rows := make([][]tgbotapi.InlineKeyboardButton, 0, len(cabs))
	for _, c := range cabs {
		rows = append(rows, []tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData(c.Name, fmt.Sprintf("cab:%d", c.ID)),
		})
	}
	out := tgbotapi.NewMessage(msg.Chat.ID, "Выберите кабинет:")
	out.ReplyMarkup = tgbotapi.InlineKeyboardMarkup{InlineKeyboard: rows}
	_, _ = b.bot.Send(out)
}

func (b *Bot) sendItems(chatID int64) {
	rows := [][]tgbotapi.InlineKeyboardButton{
		{tgbotapi.NewInlineKeyboardButtonData("Без аппарата", "item:none")},
	}
	if b.api != nil {
		items, err := b.api.ListItems(context.Background())
		if err == nil {
			for _, it := range items {
				rows = append(rows, []tgbotapi.InlineKeyboardButton{
					tgbotapi.NewInlineKeyboardButtonData(it.Name, fmt.Sprintf("item:%s", it.Name)),
				})
			}
		}
	}
	out := tgbotapi.NewMessage(chatID, "Выберите аппарат (или без аппарата):")
	out.ReplyMarkup = tgbotapi.InlineKeyboardMarkup{InlineKeyboard: rows}
	_, _ = b.bot.Send(out)
}

func (b *Bot) sendCalendar(chatID int64, cabinetID int64) {
	now := time.Now()
	markup := GenerateCalendarKeyboard(now.Year(), int(now.Month()), nil)
	out := tgbotapi.NewMessage(chatID, "Выберите дату:")
	out.ReplyMarkup = markup
	_, _ = b.bot.Send(out)
}

func (b *Bot) sendTimeSlots(ctx context.Context, chatID int64, userID int64) {
	st := b.state.get(userID)
	if st.Draft.CabinetID == 0 || st.Draft.Date == "" {
		b.reply(chatID, "Сначала выберите кабинет и дату: /book")
		return
	}
	date, err := time.Parse("2006-01-02", st.Draft.Date)
	if err != nil {
		b.reply(chatID, "Некорректная дата")
		return
	}
	slots, err := b.db.GetAvailableSlots(ctx, st.Draft.CabinetID, date)
	if err != nil {
		b.reply(chatID, "Не удалось получить слоты")
		return
	}

	ui := make([]TimeSlot, 0, len(slots))
	for _, s := range slots {
		label := fmt.Sprintf("%s-%s", s.StartTime, s.EndTime)
		ui = append(ui, TimeSlot{Label: label, CallbackData: fmt.Sprintf("slot:%s", label), Available: s.Available})
	}
	out := tgbotapi.NewMessage(chatID, "Выберите время:")
	out.ReplyMarkup = GenerateTimeSlotsKeyboard(ui, st.Draft.Date)
	_, _ = b.bot.Send(out)
}

func (b *Bot) sendConfirm(chatID int64, userID int64) {
	st := b.state.get(userID)
	item := st.Draft.ItemName
	if item == "" {
		item = "Без аппарата"
	}
	text := fmt.Sprintf("Проверьте данные:\n\nКабинет: %s\nАппарат: %s\nДата: %s\nВремя: %s\nКлиент: %s\nТелефон: %s\n\nПодтвердить?",
		st.Draft.CabinetName, item, st.Draft.Date, st.Draft.TimeLabel, st.Draft.ClientName, st.Draft.ClientPhone)

	rows := [][]tgbotapi.InlineKeyboardButton{
		{
			tgbotapi.NewInlineKeyboardButtonData("✅ Подтвердить", "confirm"),
			tgbotapi.NewInlineKeyboardButtonData("❌ Отмена", "cancel"),
		},
	}
	out := tgbotapi.NewMessage(chatID, text)
	out.ReplyMarkup = tgbotapi.InlineKeyboardMarkup{InlineKeyboard: rows}
	_, _ = b.bot.Send(out)
}

func (b *Bot) finalizeBooking(ctx context.Context, cq *tgbotapi.CallbackQuery, st *userState) error {
	if cq == nil || cq.Message == nil {
		return fmt.Errorf("missing callback message")
	}
	// ensure user exists
	u, err := b.db.GetOrCreateUserByTelegramID(ctx, cq.From.ID, cq.From.UserName, cq.From.FirstName, cq.From.LastName)
	if err != nil {
		return err
	}

	date, err := time.Parse("2006-01-02", st.Draft.Date)
	if err != nil {
		return err
	}
	start, end, err := parseTimeLabel(date, st.Draft.TimeLabel)
	if err != nil {
		return err
	}
	if err := b.validateBookingTime(start); err != nil {
		return err
	}

	bk := &models.HourlyBooking{
		UserID:      u.ID,
		CabinetID:   st.Draft.CabinetID,
		ItemName:    st.Draft.ItemName,
		ClientName:  st.Draft.ClientName,
		ClientPhone: st.Draft.ClientPhone,
		StartTime:   start,
		EndTime:     end,
		Status:      "pending",
		Comment:     "",
	}

	if err := b.db.CreateHourlyBookingWithChecks(ctx, bk, b.api); err != nil {
		return err
	}

	item := bk.ItemName
	if item == "" {
		item = "Без аппарата"
	}
	b.reply(cq.Message.Chat.ID, fmt.Sprintf("Заявка #%d создана. Кабинет: %s, %s %s, %s", bk.ID, st.Draft.CabinetName, st.Draft.Date, st.Draft.TimeLabel, item))
	b.notifyManagersNewBooking(bk.ID, st.Draft.CabinetName, item, st.Draft.Date, st.Draft.TimeLabel, st.Draft.ClientName, st.Draft.ClientPhone)
	return nil
}

func parseTimeLabel(date time.Time, label string) (time.Time, time.Time, error) {
	parts := strings.Split(label, "-")
	if len(parts) != 2 {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid time label")
	}
	startStr := strings.TrimSpace(parts[0])
	endStr := strings.TrimSpace(parts[1])
	start, err := time.Parse("15:04", startStr)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	end, err := time.Parse("15:04", endStr)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	startDT := time.Date(date.Year(), date.Month(), date.Day(), start.Hour(), start.Minute(), 0, 0, time.Local)
	endDT := time.Date(date.Year(), date.Month(), date.Day(), end.Hour(), end.Minute(), 0, 0, time.Local)
	return startDT, endDT, nil
}

func normalizeAndValidatePhone(raw string) (string, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", false
	}
	// allow + and digits; strip common separators
	repl := strings.NewReplacer(" ", "", "-", "", "(", "", ")", "", "\t", "")
	s = repl.Replace(s)
	if strings.HasPrefix(s, "+") {
		s = "+" + filterDigits(s[1:])
	} else {
		s = filterDigits(s)
	}
	// very rough length check; keeps UX simple
	digits := strings.TrimPrefix(s, "+")
	if len(digits) < 10 || len(digits) > 15 {
		return "", false
	}
	if s[0] != '+' {
		// assume local; keep digits-only
		return digits, true
	}
	return s, true
}

func filterDigits(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func (b *Bot) notifyManagersNewBooking(id int64, cabinet, item, date, timeLabel, clientName, clientPhone string) {
	rows := [][]tgbotapi.InlineKeyboardButton{
		{
			tgbotapi.NewInlineKeyboardButtonData("✅ Approve", fmt.Sprintf("mgr:approve:%d", id)),
			tgbotapi.NewInlineKeyboardButtonData("❌ Reject", fmt.Sprintf("mgr:reject:%d", id)),
		},
	}
	text := fmt.Sprintf("Новая заявка #%d\nКабинет: %s\nАппарат: %s\nДата: %s\nВремя: %s\nКлиент: %s\nТелефон: %s", id, cabinet, item, date, timeLabel, clientName, clientPhone)
	for mgrID := range b.managers {
		msg := tgbotapi.NewMessage(mgrID, text)
		msg.ReplyMarkup = tgbotapi.InlineKeyboardMarkup{InlineKeyboard: rows}
		_, _ = b.bot.Send(msg)
	}
}

func (b *Bot) notifyBookingStatus(ctx context.Context, bookingID int64, status string) {
	// best effort: load booking + user telegram id
	row := b.db.QueryRowContext(ctx, `SELECT u.telegram_id FROM hourly_bookings hb JOIN users u ON u.id = hb.user_id WHERE hb.id = ?`, bookingID)
	var telegramID int64
	if err := row.Scan(&telegramID); err != nil {
		return
	}
	msg := tgbotapi.NewMessage(telegramID, fmt.Sprintf("Статус заявки #%d: %s", bookingID, status))
	_, _ = b.bot.Send(msg)
}
