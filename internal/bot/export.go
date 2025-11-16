package bot

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"bronivik/internal/models"
	"github.com/xuri/excelize/v2"
)

// exportToExcel создает Excel файл с данными о бронированиях
// exportToExcel создает Excel файл с данными о бронированиях
func (b *Bot) exportToExcel(startDate, endDate time.Time) (string, error) {
	// Создаем папку для экспорта, если не существует
	if err := os.MkdirAll(b.config.Exports.Path, 0755); err != nil {
		return "", fmt.Errorf("error creating export directory: %v", err)
	}

	// Получаем данные из БД
	dailyBookings, err := b.db.GetDailyBookings(context.Background(), startDate, endDate)
	if err != nil {
		return "", fmt.Errorf("error getting bookings: %v", err)
	}

	items := b.items

	// Создаем новый Excel файл
	f := excelize.NewFile()

	// Создаем лист с данными
	index, err := f.NewSheet("Бронирования")
	if err != nil {
		return "", fmt.Errorf("error creating sheet: %v", err)
	}
	f.SetActiveSheet(index)

	// Устанавливаем заголовок периода
	f.SetCellValue("Бронирования", "A1", fmt.Sprintf("Период: %s - %s",
		startDate.Format("02.01.2006"), endDate.Format("02.01.2006")))

	// Заголовки - даты (начинаем с строки 2)
	col := 2
	currentDate := startDate
	dateHeaders := make(map[string]int)

	for !currentDate.After(endDate) {
		cell, _ := excelize.CoordinatesToCellName(col, 2)
		dateStr := currentDate.Format("02.01")
		f.SetCellValue("Бронирования", cell, dateStr)
		dateHeaders[currentDate.Format("2006-01-02")] = col

		// Форматируем заголовки дат
		style, _ := f.NewStyle(&excelize.Style{
			Fill:      excelize.Fill{Type: "pattern", Color: []string{"#DDEBF7"}, Pattern: 1},
			Font:      &excelize.Font{Bold: true},
			Alignment: &excelize.Alignment{Horizontal: "center"},
		})
		f.SetCellStyle("Бронирования", cell, cell, style)

		col++
		currentDate = currentDate.AddDate(0, 0, 1)
	}

	// Названия аппаратов в первом столбце
	row := 3
	for _, item := range items {
		cell, _ := excelize.CoordinatesToCellName(1, row)
		f.SetCellValue("Бронирования", cell, fmt.Sprintf("%s (%d)", item.Name, item.TotalQuantity))

		style, _ := f.NewStyle(&excelize.Style{
			Fill: excelize.Fill{Type: "pattern", Color: []string{"#E2EFDA"}, Pattern: 1},
			Font: &excelize.Font{Bold: true},
		})
		f.SetCellStyle("Бронирования", cell, cell, style)

		row++
	}

	// Заполняем данные по бронированиям
	for dateKey, bookings := range dailyBookings {
		col, exists := dateHeaders[dateKey]
		if !exists {
			continue
		}

		// Группируем бронирования по аппаратам
		bookingsByItem := make(map[int64][]models.Booking)
		for _, booking := range bookings {
			bookingsByItem[booking.ItemID] = append(bookingsByItem[booking.ItemID], booking)
		}

		// Заполняем данные для каждого аппарата
		row := 3
		for _, item := range items {
			cell, _ := excelize.CoordinatesToCellName(col, row)
			itemBookings := bookingsByItem[item.ID]

			// Получаем количество занятых аппаратов (только активные заявки)
			bookedCount, err := b.db.GetBookedCount(context.Background(), item.ID, parseDate(dateKey))
			if err != nil {
				log.Printf("Error getting booked count: %v", err)
				bookedCount = 0
			}

			if len(itemBookings) > 0 {
				var cellValue string
				for _, booking := range itemBookings {
					status := "❓"
					switch booking.Status {
					case "confirmed", "completed":
						status = "✅"
					case "pending", "changed":
						status = "⏳"
					case "cancelled":
						status = "❌"
					}
					cellValue += fmt.Sprintf("%s %s (%s)\n", status, booking.UserName, booking.Phone)
					if booking.Comment != "" {
						cellValue += fmt.Sprintf("   💬 %s\n", booking.Comment)
					}
				}
				cellValue += fmt.Sprintf("\nЗанято: %d/%d", bookedCount, item.TotalQuantity)
				f.SetCellValue("Бронирования", cell, cellValue)
			} else {
				cellValue := fmt.Sprintf("Свободно\n\nДоступно: %d/%d", item.TotalQuantity, item.TotalQuantity)
				f.SetCellValue("Бронирования", cell, cellValue)
			}

			// Определяем цвет заливки
			styleID, err := b.getCellStyle(f, itemBookings, bookedCount, int(item.TotalQuantity))
			if err == nil {
				f.SetCellStyle("Бронирования", cell, cell, styleID)
			}

			row++
		}
	}

	// Настраиваем ширину колонок
	f.SetColWidth("Бронирования", "A", "A", 25)
	for i := 'B'; i < 'Z'; i++ {
		f.SetColWidth("Бронирования", string(i), string(i), 20)
	}

	// Объединяем ячейку для заголовка периода
	lastCol := getLastColumn(len(dateHeaders) + 1)
	f.MergeCell("Бронирования", "A1", lastCol+"1")

	// Стиль для заголовка периода
	style, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Size: 14},
		Alignment: &excelize.Alignment{Horizontal: "center"},
	})
	f.SetCellStyle("Бронирования", "A1", "A1", style)

	// Удаляем стандартный лист
	f.DeleteSheet("Sheet1")

	// Сохраняем файл
	fileName := fmt.Sprintf("export_%s_to_%s.xlsx",
		startDate.Format("2006-01-02"),
		endDate.Format("2006-01-02"))
	filePath := filepath.Join(b.config.Exports.Path, fileName)

	if err := f.SaveAs(filePath); err != nil {
		return "", fmt.Errorf("error saving file: %v", err)
	}

	log.Printf("Excel file created: %s", filePath)
	return filePath, nil
}

// parseDate преобразует строку в time.Time
func parseDate(dateStr string) time.Time {
	date, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return time.Now()
	}
	return date
}

// getCellStyle возвращает стиль ячейки
func (b *Bot) getCellStyle(f *excelize.File, itemBookings []models.Booking, bookedCount int, totalQuantity int) (int, error) {
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
		if booking.Status == "pending" || booking.Status == "changed" {
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
func (b *Bot) filterActiveBookings(bookings []models.Booking) []models.Booking {
	var active []models.Booking
	for _, booking := range bookings {
		if booking.Status != "cancelled" {
			active = append(active, booking)
		}
	}
	return active
}

// getLastColumn возвращает последнюю колонку для объединения ячеек
func getLastColumn(colCount int) string {
	// Базовые колонки A-Z
	if colCount <= 26 {
		return string('A' + colCount - 1)
	}

	// Для большего количества колонок (AA, AB, etc.)
	firstChar := string('A' + (colCount-1)/26 - 1)
	secondChar := string('A' + (colCount-1)%26)
	return firstChar + secondChar
}

// exportUsersToExcel создает Excel файл с данными пользователей
func (b *Bot) exportUsersToExcel(users []models.User) (string, error) {
	// Создаем папку для экспорта, если не существует
	if err := os.MkdirAll(b.config.Exports.Path, 0755); err != nil {
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
	headers := []string{"ID", "Telegram ID", "Username", "Имя", "Фамилия", "Телефон", "Менеджер", "Черный список", "Язык", "Последняя активность", "Дата регистрации"}
	for i, header := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		f.SetCellValue("Пользователи", cell, header)
		// f.SetCellStyle("Пользователи", cell, cell, f.SetCellStyle("Пользователи", cell, "bold")
	}

	// Данные пользователей
	for i, user := range users {
		row := i + 2
		f.SetCellValue("Пользователи", fmt.Sprintf("A%d", row), user.ID)
		f.SetCellValue("Пользователи", fmt.Sprintf("B%d", row), user.TelegramID)
		f.SetCellValue("Пользователи", fmt.Sprintf("C%d", row), user.Username)
		f.SetCellValue("Пользователи", fmt.Sprintf("D%d", row), user.FirstName)
		f.SetCellValue("Пользователи", fmt.Sprintf("E%d", row), user.LastName)
		f.SetCellValue("Пользователи", fmt.Sprintf("F%d", row), user.Phone)
		f.SetCellValue("Пользователи", fmt.Sprintf("G%d", row), boolToYesNo(user.IsManager))
		f.SetCellValue("Пользователи", fmt.Sprintf("H%d", row), boolToYesNo(user.IsBlacklisted))
		f.SetCellValue("Пользователи", fmt.Sprintf("I%d", row), user.LanguageCode)
		f.SetCellValue("Пользователи", fmt.Sprintf("J%d", row), user.LastActivity.Format("02.01.2006 15:04"))
		f.SetCellValue("Пользователи", fmt.Sprintf("K%d", row), user.CreatedAt.Format("02.01.2006 15:04"))
	}

	// Настраиваем ширину колонок
	f.SetColWidth("Пользователи", "A", "A", 10)
	f.SetColWidth("Пользователи", "B", "B", 15)
	f.SetColWidth("Пользователи", "C", "C", 20)
	f.SetColWidth("Пользователи", "D", "D", 15)
	f.SetColWidth("Пользователи", "E", "E", 15)
	f.SetColWidth("Пользователи", "F", "F", 15)
	f.SetColWidth("Пользователи", "G", "G", 10)
	f.SetColWidth("Пользователи", "H", "H", 12)
	f.SetColWidth("Пользователи", "I", "I", 10)
	f.SetColWidth("Пользователи", "J", "J", 20)
	f.SetColWidth("Пользователи", "K", "K", 20)

	// Удаляем стандартный лист
	f.DeleteSheet("Sheet1")

	// Сохраняем файл
	fileName := fmt.Sprintf("users_export_%s.xlsx", time.Now().Format("2006-01-02_15-04-05"))
	filePath := filepath.Join(b.config.Exports.Path, fileName)

	if err := f.SaveAs(filePath); err != nil {
		return "", fmt.Errorf("error saving file: %v", err)
	}

	log.Printf("Users Excel file created: %s", filePath)
	return filePath, nil
}

// boolToYesNo преобразует bool в "Да"/"Нет"
func boolToYesNo(b bool) string {
	if b {
		return "Да"
	}
	return "Нет"
}
