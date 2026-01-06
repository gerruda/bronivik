package bot

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"bronivik/internal/models"

	"github.com/xuri/excelize/v2"
)

// exportToExcel создает Excel файл с данными о бронированиях
func (b *Bot) exportToExcel(ctx context.Context, startDate, endDate time.Time) (string, error) {
	// Создаем папку для экспорта, если не существует
	if err := os.MkdirAll(b.config.Exports.Path, 0o755); err != nil {
		return "", fmt.Errorf("error creating export directory: %v", err)
	}

	// Получаем данные из БД
	dailyBookings, err := b.bookingService.GetDailyBookings(ctx, startDate, endDate)
	if err != nil {
		return "", fmt.Errorf("error getting bookings: %v", err)
	}

	items, err := b.itemService.GetActiveItems(ctx)
	if err != nil {
		return "", fmt.Errorf("error getting active items: %v", err)
	}

	// Создаем новый Excel файл
	f := excelize.NewFile()
	defer f.Close()

	// Создаем лист с данными
	sheetName := "Бронирования"
	index, err := f.NewSheet(sheetName)
	if err != nil {
		return "", fmt.Errorf("error creating sheet: %v", err)
	}
	f.SetActiveSheet(index)

	// Устанавливаем заголовок периода
	_ = f.SetCellValue(sheetName, "A1", fmt.Sprintf("Период: %s - %s",
		startDate.Format("02.01.2006"), endDate.Format("02.01.2006")))

	// Заголовки - даты
	dateHeaders := b.writeDateHeaders(f, sheetName, startDate, endDate)

	// Названия аппаратов
	b.writeItemHeaders(f, sheetName, items)

	// Заполняем данные по бронированиям
	b.writeBookingData(ctx, f, sheetName, dailyBookings, items, dateHeaders)

	// Настраиваем ширину колонок
	_ = f.SetColWidth(sheetName, "A", "A", 25)
	for i := 'B'; i <= 'Z'; i++ {
		_ = f.SetColWidth(sheetName, string(i), string(i), 20)
	}

	// Объединяем ячейку для заголовка периода
	lastCol := getLastColumn(len(dateHeaders) + 1)
	_ = f.MergeCell(sheetName, "A1", lastCol+"1")

	// Стиль для заголовка периода
	style, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Size: 14},
		Alignment: &excelize.Alignment{Horizontal: "center"},
	})
	_ = f.SetCellStyle(sheetName, "A1", "A1", style)

	// Удаляем стандартный лист
	_ = f.DeleteSheet("Sheet1")

	// Сохраняем файл
	fileName := fmt.Sprintf("export_%s_to_%s.xlsx",
		startDate.Format("2006-01-02"),
		endDate.Format("2006-01-02"))
	filePath := filepath.Join(b.config.Exports.Path, fileName)

	if err := f.SaveAs(filePath); err != nil {
		return "", fmt.Errorf("error saving file: %v", err)
	}

	b.logger.Info().Str("file_path", filePath).Msg("Excel file created")
	return filePath, nil
}

func (b *Bot) writeDateHeaders(f *excelize.File, sheetName string, startDate, endDate time.Time) map[string]int {
	col := 2
	currentDate := startDate
	dateHeaders := make(map[string]int)

	for !currentDate.After(endDate) {
		cell, _ := excelize.CoordinatesToCellName(col, 2)
		dateStr := currentDate.Format("02.01")
		_ = f.SetCellValue(sheetName, cell, dateStr)
		dateHeaders[currentDate.Format("2006-01-02")] = col

		style, _ := f.NewStyle(&excelize.Style{
			Fill:      excelize.Fill{Type: "pattern", Color: []string{"#DDEBF7"}, Pattern: 1},
			Font:      &excelize.Font{Bold: true},
			Alignment: &excelize.Alignment{Horizontal: "center"},
		})
		_ = f.SetCellStyle(sheetName, cell, cell, style)

		col++
		currentDate = currentDate.AddDate(0, 0, 1)
	}
	return dateHeaders
}

func (b *Bot) writeItemHeaders(f *excelize.File, sheetName string, items []*models.Item) {
	row := 3
	for _, item := range items {
		cell, _ := excelize.CoordinatesToCellName(1, row)
		_ = f.SetCellValue(sheetName, cell, fmt.Sprintf("%s (%d)", item.Name, item.TotalQuantity))

		style, _ := f.NewStyle(&excelize.Style{
			Fill: excelize.Fill{Type: "pattern", Color: []string{"#E2EFDA"}, Pattern: 1},
			Font: &excelize.Font{Bold: true},
		})
		_ = f.SetCellStyle(sheetName, cell, cell, style)

		row++
	}
}

func (b *Bot) writeBookingData(
	ctx context.Context, f *excelize.File, sheetName string,
	dailyBookings map[string][]*models.Booking,
	items []*models.Item,
	dateHeaders map[string]int,
) {
	for dateKey, bookings := range dailyBookings {
		col, exists := dateHeaders[dateKey]
		if !exists {
			continue
		}

		bookingsByItem := make(map[int64][]*models.Booking)
		for _, booking := range bookings {
			bookingsByItem[booking.ItemID] = append(bookingsByItem[booking.ItemID], booking)
		}

		row := 3
		for _, item := range items {
			cell, _ := excelize.CoordinatesToCellName(col, row)
			itemBookings := bookingsByItem[item.ID]

			bookedCount, err := b.bookingService.GetBookedCount(ctx, item.ID, parseDate(dateKey))
			if err != nil {
				b.logger.Error().Err(err).Int64("item_id", item.ID).Str("date", dateKey).Msg("Error getting booked count")
				bookedCount = 0
			}

			var cellValue string
			if len(itemBookings) > 0 {
				for _, booking := range itemBookings {
					status := b.getBookingStatusIcon(booking.Status)
					cellValue += fmt.Sprintf("%s %s (%s)\n", status, booking.UserName, booking.Phone)
					if booking.Comment != "" {
						cellValue += fmt.Sprintf("   💬 %s\n", booking.Comment)
					}
				}
				cellValue += fmt.Sprintf("\nЗанято: %d/%d", bookedCount, item.TotalQuantity)
			} else {
				cellValue = fmt.Sprintf("Свободно\n\nДоступно: %d/%d", item.TotalQuantity, item.TotalQuantity)
			}

			_ = f.SetCellValue(sheetName, cell, cellValue)

			styleID, err := b.getCellStyle(f, itemBookings, bookedCount, int(item.TotalQuantity))
			if err == nil {
				_ = f.SetCellStyle(sheetName, cell, cell, styleID)
			}
			row++
		}
	}
}

func (b *Bot) getBookingStatusIcon(status string) string {
	switch status {
	case models.StatusConfirmed, models.StatusCompleted:
		return statusSuccess
	case models.StatusPending, models.StatusChanged:
		return statusPending
	case models.StatusCanceled:
		return statusError
	default:
		return "❓"
	}
}

// parseDate преобразует строку в time.Time
func parseDate(dateStr string) time.Time {
	date, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return time.Time{}
	}
	return date
}

// getCellStyle возвращает стиль ячейки
func (b *Bot) getCellStyle(f *excelize.File, itemBookings []*models.Booking, bookedCount, totalQuantity int) (int, error) {
	// Фильтруем активные заявки (исключаем отмененные)
	activeBookings := b.filterActiveBookings(itemBookings)

	// 1. Если нет активных заявок - БЕЗ ЗАЛИВКИ
	if len(activeBookings) == 0 {
		style, err := f.NewStyle(&excelize.Style{
			Fill: excelize.Fill{Type: "pattern", Color: []string{"#FFFFFF"}, Pattern: 1},
			Alignment: &excelize.Alignment{
				Horizontal: "left",
				Vertical:   "top",
				WrapText:   true,
			},
		})
		return style, err
	}

	// 2. Если все аппараты заняты - КРАСНЫЙ
	if bookedCount >= totalQuantity {
		style, err := f.NewStyle(&excelize.Style{
			Fill: excelize.Fill{Type: "pattern", Color: []string{"#FFC7CE"}, Pattern: 1},
			Alignment: &excelize.Alignment{
				Horizontal: "left",
				Vertical:   "top",
				WrapText:   true,
			},
		})
		return style, err
	}

	// 3. Проверяем статусы активных заявок
	hasUnconfirmed := false
	for _, booking := range activeBookings {
		if booking.Status == models.StatusPending || booking.Status == models.StatusChanged {
			hasUnconfirmed = true
			break
		}
	}

	// 4. Если есть неподтвержденные заявки - ЖЕЛТЫЙ
	if hasUnconfirmed {
		style, err := f.NewStyle(&excelize.Style{
			Fill: excelize.Fill{Type: "pattern", Color: []string{"#FFEB9C"}, Pattern: 1},
			Alignment: &excelize.Alignment{
				Horizontal: "left",
				Vertical:   "top",
				WrapText:   true,
			},
		})
		return style, err
	}

	// 5. Если все заявки подтверждены - ЗЕЛЕНЫЙ
	style, err := f.NewStyle(&excelize.Style{
		Fill: excelize.Fill{Type: "pattern", Color: []string{"#C6EFCE"}, Pattern: 1},
		Alignment: &excelize.Alignment{
			Horizontal: "left",
			Vertical:   "top",
			WrapText:   true,
		},
	})
	return style, err
}

// filterActiveBookings фильтрует активные заявки
func (b *Bot) filterActiveBookings(bookings []*models.Booking) []*models.Booking {
	var active []*models.Booking
	for _, booking := range bookings {
		if booking.Status != models.StatusCanceled {
			active = append(active, booking)
		}
	}
	return active
}

// getLastColumn возвращает последнюю колонку для объединения ячеек
func getLastColumn(colCount int) string {
	// Базовые колонки A-Z
	if colCount <= 26 {
		return string(rune('A' + colCount - 1))
	}

	// Для большего количества колонок (AA, AB, etc.)
	firstChar := string(rune('A' + (colCount-1)/26 - 1))
	secondChar := string(rune('A' + (colCount-1)%26))
	return firstChar + secondChar
}

// exportUsersToExcel создает Excel файл с данными пользователей
func (b *Bot) exportUsersToExcel(_ context.Context, users []*models.User) (string, error) {
	// Создаем папку для экспорта, если не существует
	if err := os.MkdirAll(b.config.Exports.Path, 0o755); err != nil {
		return "", fmt.Errorf("error creating export directory: %v", err)
	}

	// Создаем новый Excel файл
	f := excelize.NewFile()

	// Создаем лист с пользователями
	index, err := f.NewSheet("Пользователи")
	if err != nil {
		return "", fmt.Errorf("error creating sheet: %v", err)
	}
	f.SetActiveSheet(index)

	// Заголовки
	headers := []string{
		"ID", "Telegram ID", "Username", "Имя", "Фамилия", "Телефон",
		"Менеджер", "Черный список", "Язык", "Последняя активность", "Дата регистрации",
	}
	for i, header := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		_ = f.SetCellValue("Пользователи", cell, header)
		// f.SetCellStyle("Пользователи", cell, cell, f.SetCellStyle("Пользователи", cell, "bold")
	}

	// Данные пользователей
	for i, user := range users {
		row := i + 2
		_ = f.SetCellValue("Пользователи", fmt.Sprintf("A%d", row), user.ID)
		_ = f.SetCellValue("Пользователи", fmt.Sprintf("B%d", row), user.TelegramID)
		_ = f.SetCellValue("Пользователи", fmt.Sprintf("C%d", row), user.Username)
		_ = f.SetCellValue("Пользователи", fmt.Sprintf("D%d", row), user.FirstName)
		_ = f.SetCellValue("Пользователи", fmt.Sprintf("E%d", row), user.LastName)
		_ = f.SetCellValue("Пользователи", fmt.Sprintf("F%d", row), user.Phone)
		_ = f.SetCellValue("Пользователи", fmt.Sprintf("G%d", row), boolToYesNo(user.IsManager))
		_ = f.SetCellValue("Пользователи", fmt.Sprintf("H%d", row), boolToYesNo(user.IsBlacklisted))
		_ = f.SetCellValue("Пользователи", fmt.Sprintf("I%d", row), user.LanguageCode)
		_ = f.SetCellValue("Пользователи", fmt.Sprintf("J%d", row), user.LastActivity.Format("02.01.2006 15:04"))
		_ = f.SetCellValue("Пользователи", fmt.Sprintf("K%d", row), user.CreatedAt.Format("02.01.2006 15:04"))
	}

	// Настраиваем ширину колонок
	_ = f.SetColWidth("Пользователи", "A", "A", 10)
	_ = f.SetColWidth("Пользователи", "B", "B", 15)
	_ = f.SetColWidth("Пользователи", "C", "C", 20)
	_ = f.SetColWidth("Пользователи", "D", "D", 15)
	_ = f.SetColWidth("Пользователи", "E", "E", 15)
	_ = f.SetColWidth("Пользователи", "F", "F", 15)
	_ = f.SetColWidth("Пользователи", "G", "G", 10)
	_ = f.SetColWidth("Пользователи", "H", "H", 12)
	_ = f.SetColWidth("Пользователи", "I", "I", 10)
	_ = f.SetColWidth("Пользователи", "J", "J", 20)
	_ = f.SetColWidth("Пользователи", "K", "K", 20)

	// Удаляем стандартный лист
	_ = f.DeleteSheet("Sheet1")

	// Сохраняем файл
	fileName := fmt.Sprintf("users_export_%s.xlsx", time.Now().Format("2006-01-02_15-04-05"))
	filePath := filepath.Join(b.config.Exports.Path, fileName)

	if err := f.SaveAs(filePath); err != nil {
		return "", fmt.Errorf("error saving file: %v", err)
	}

	b.logger.Info().Str("file_path", filePath).Msg("Users Excel file created")
	return filePath, nil
}

// boolToYesNo преобразует bool в "Да"/"Нет"
func boolToYesNo(b bool) string {
	if b {
		return "Да"
	}
	return "Нет"
}
