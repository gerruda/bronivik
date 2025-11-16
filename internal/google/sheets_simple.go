package google

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"bronivik/internal/models"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"
)

type SheetsService struct {
	service         *sheets.Service
	usersSheetID    string
	bookingsSheetID string
}

func NewSimpleSheetsService(credentialsFile, usersSheetID, bookingsSheetID string) (*SheetsService, error) {
	ctx := context.Background()

	// Читаем файл учетных данных сервисного аккаунта
	credentialsJSON, err := os.ReadFile(credentialsFile)
	if err != nil {
		return nil, fmt.Errorf("unable to read credentials file: %v", err)
	}

	// Создаем JWT конфигурацию
	config, err := google.JWTConfigFromJSON(credentialsJSON, sheets.SpreadsheetsScope)
	if err != nil {
		return nil, fmt.Errorf("unable to parse credentials: %v", err)
	}

	// Создаем клиент
	client := config.Client(ctx)

	// Создаем сервис
	srv, err := sheets.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return nil, fmt.Errorf("unable to create Sheets service: %v", err)
	}

	return &SheetsService{
		service:         srv,
		usersSheetID:    usersSheetID,
		bookingsSheetID: bookingsSheetID,
	}, nil
}

// TestConnection проверяет подключение к таблице
func (s *SheetsService) TestConnection() error {
	// Пробуем прочитать первую ячейку таблицы пользователей
	_, err := s.service.Spreadsheets.Values.Get(s.usersSheetID, "Users!A1").Do()
	if err != nil {
		return fmt.Errorf("connection test failed: %v", err)
	}
	return nil
}

// GetServiceAccountEmail возвращает email сервисного аккаунта
func (s *SheetsService) GetServiceAccountEmail(credentialsFile string) (string, error) {
	file, err := os.ReadFile(credentialsFile)
	if err != nil {
		return "", err
	}

	var creds struct {
		ClientEmail string `json:"client_email"`
	}

	if err := json.Unmarshal(file, &creds); err != nil {
		return "", err
	}

	return creds.ClientEmail, nil
}

// UpdateUsersSheet обновляет таблицу пользователей
func (s *SheetsService) UpdateUsersSheet(users []*models.User) error {
	// Подготавливаем данные
	var values [][]interface{}

	// Заголовки
	headers := []interface{}{"ID", "Telegram ID", "Username", "First Name", "Last Name", "Phone", "Is Manager", "Is Blacklisted", "Language Code", "Last Activity", "Created At"}
	values = append(values, headers)

	// Данные пользователей
	for _, user := range users {
		row := []interface{}{
			user.ID,
			user.TelegramID,
			user.Username,
			user.FirstName,
			user.LastName,
			user.Phone,
			user.IsManager,
			user.IsBlacklisted,
			user.LanguageCode,
			user.LastActivity.Format("2006-01-02 15:04:05"),
			user.CreatedAt.Format("2006-01-02 15:04:05"),
		}
		values = append(values, row)
	}

	// Полностью очищаем и перезаписываем лист
	rangeData := "Users!A1:K" + fmt.Sprintf("%d", len(values))
	valueRange := &sheets.ValueRange{
		Values: values,
	}

	// Используем Overwrite для полной замены данных
	_, err := s.service.Spreadsheets.Values.Update(s.usersSheetID, rangeData, valueRange).
		ValueInputOption("RAW").
		Do()

	return err
}

// AppendBooking добавляет новое бронирование
func (s *SheetsService) AppendBooking(booking *models.Booking) error {
	row := []interface{}{
		booking.ID,
		booking.UserID,
		booking.ItemID,
		booking.Date.Format("2006-01-02"),
		booking.Status,
		booking.UserName,
		booking.Phone,
		booking.ItemName,
		booking.CreatedAt.Format("2006-01-02 15:04:05"),
		booking.UpdatedAt.Format("2006-01-02 15:04:05"),
	}

	rangeData := "Bookings!A:A"
	valueRange := &sheets.ValueRange{
		Values: [][]interface{}{row},
	}

	_, err := s.service.Spreadsheets.Values.Append(s.bookingsSheetID, rangeData, valueRange).
		ValueInputOption("RAW").
		InsertDataOption("INSERT_ROWS").
		Do()

	return err
}

// UpdateBookingsSheet обновляет всю таблицу бронирований
func (s *SheetsService) UpdateBookingsSheet(bookings []*models.Booking) error {
	var values [][]interface{}

	// Заголовки
	headers := []interface{}{"ID", "User ID", "Item ID", "Date", "Status", "User Name", "User Phone", "Item Name", "Created At", "Updated At"}
	values = append(values, headers)

	// Данные бронирований
	for _, booking := range bookings {
		row := []interface{}{
			booking.ID,
			booking.UserID,
			booking.ItemID,
			booking.Date.Format("2006-01-02"),
			booking.Status,
			booking.UserName,
			booking.Phone,
			booking.ItemName,
			booking.CreatedAt.Format("2006-01-02 15:04:05"),
			booking.UpdatedAt.Format("2006-01-02 15:04:05"),
		}
		values = append(values, row)
	}

	// Полностью очищаем и перезаписываем лист
	rangeData := "Bookings!A1:J" + fmt.Sprintf("%d", len(values))
	valueRange := &sheets.ValueRange{
		Values: values,
	}

	_, err := s.service.Spreadsheets.Values.Update(s.bookingsSheetID, rangeData, valueRange).
		ValueInputOption("RAW").
		Do()

	return err
}

// UpdateScheduleSheet обновляет лист с расписанием бронирований в формате таблицы
func (s *SheetsService) UpdateScheduleSheet(startDate, endDate time.Time, dailyBookings map[string][]models.Booking, items []models.Item) error {
	// Получаем ID листа "Бронирования"
	sheetId, err := s.GetSheetIdByName(s.bookingsSheetID, "Бронирования")
	if err != nil {
		return fmt.Errorf("unable to get sheet ID: %v", err)
	}

	// Очищаем весь лист "Бронирования"
	clearRange := "Бронирования!A:Z"
	_, err = s.service.Spreadsheets.Values.Clear(s.bookingsSheetID, clearRange, &sheets.ClearValuesRequest{}).Do()
	if err != nil {
		return fmt.Errorf("unable to clear sheet: %v", err)
	}

	var data [][]interface{}
	var formatRequests []*sheets.Request

	// Заголовок периода (строка 1)
	data = append(data, []interface{}{
		fmt.Sprintf("Период: %s - %s",
			startDate.Format("02.01.2006"),
			endDate.Format("02.01.2006")),
	})

	// Форматирование заголовка периода
	formatRequests = append(formatRequests, &sheets.Request{
		RepeatCell: &sheets.RepeatCellRequest{
			Range: &sheets.GridRange{
				SheetId:          sheetId,
				StartRowIndex:    0,
				EndRowIndex:      1,
				StartColumnIndex: 0,
				EndColumnIndex:   1,
			},
			Cell: &sheets.CellData{
				UserEnteredFormat: &sheets.CellFormat{
					HorizontalAlignment: "CENTER",
					TextFormat: &sheets.TextFormat{
						Bold:     true,
						FontSize: 14,
					},
				},
			},
			Fields: "userEnteredFormat(textFormat,horizontalAlignment)",
		},
	})

	// Объединяем ячейки для заголовка периода
	dateCount := int(endDate.Sub(startDate).Hours()/24) + 1
	formatRequests = append(formatRequests, &sheets.Request{
		MergeCells: &sheets.MergeCellsRequest{
			Range: &sheets.GridRange{
				SheetId:          sheetId,
				StartRowIndex:    0,
				EndRowIndex:      1,
				StartColumnIndex: 0,
				EndColumnIndex:   int64(dateCount + 1),
			},
			MergeType: "MERGE_ALL",
		},
	})

	// Пустая строка между заголовком и таблицей
	data = append(data, []interface{}{})

	// Заголовки дат (строка 3)
	dateHeaders := make(map[string]int)
	headerRow := []interface{}{""}

	col := 1
	currentDate := startDate
	for !currentDate.After(endDate) {
		dateStr := currentDate.Format("02.01")
		headerRow = append(headerRow, dateStr)
		dateHeaders[currentDate.Format("2006-01-02")] = col
		col++
		currentDate = currentDate.AddDate(0, 0, 1)
	}
	data = append(data, headerRow)

	// Форматирование заголовков дат
	if len(headerRow) > 1 {
		formatRequests = append(formatRequests, &sheets.Request{
			RepeatCell: &sheets.RepeatCellRequest{
				Range: &sheets.GridRange{
					SheetId:          sheetId,
					StartRowIndex:    2,
					EndRowIndex:      3,
					StartColumnIndex: 1,
					EndColumnIndex:   int64(len(headerRow)),
				},
				Cell: &sheets.CellData{
					UserEnteredFormat: &sheets.CellFormat{
						HorizontalAlignment: "CENTER",
						TextFormat: &sheets.TextFormat{
							Bold: true,
						},
						BackgroundColor: &sheets.Color{
							Red:   0.86,
							Green: 0.92,
							Blue:  0.97,
						},
					},
				},
				Fields: "userEnteredFormat(backgroundColor,textFormat,horizontalAlignment)",
			},
		})
	}

	// Данные по аппаратам
	for rowIndex, item := range items {
		rowData := []interface{}{fmt.Sprintf("%s (%d)", item.Name, item.TotalQuantity)}

		currentDate = startDate
		for colIndex := 0; colIndex < len(dateHeaders); colIndex++ {
			dateKey := currentDate.Format("2006-01-02")
			bookings := dailyBookings[dateKey]

			var itemBookings []models.Booking
			for _, booking := range bookings {
				if booking.ItemID == item.ID {
					itemBookings = append(itemBookings, booking)
				}
			}

			cellValue := ""
			var backgroundColor *sheets.Color

			// Фильтруем активные заявки (исключаем отмененные)
			activeBookings := s.filterActiveBookings(itemBookings)
			bookedCount := len(activeBookings)

			if len(activeBookings) > 0 {
				// Есть активные заявки
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

					cellValue += fmt.Sprintf("[№%d] %s %s (%s)\n",
						booking.ID, status, booking.UserName, booking.Phone)

					// Добавляем комментарий если есть
					if booking.Comment != "" {
						cellValue += fmt.Sprintf("   💬 %s\n", booking.Comment)
					}
				}

				cellValue += fmt.Sprintf("\nЗанято: %d/%d", bookedCount, item.TotalQuantity)

				// НОВАЯ ЛОГИКА ПОДСВЕТКИ:
				// 1. Если все аппараты заняты - КРАСНЫЙ
				if bookedCount >= int(item.TotalQuantity) {
					backgroundColor = &sheets.Color{
						Red:   1.0, // #FFC7CE
						Green: 0.78,
						Blue:  0.81,
					}
				} else {
					// Проверяем статусы заявок
					hasUnconfirmed := false
					for _, booking := range activeBookings {
						if booking.Status == "pending" || booking.Status == "changed" {
							hasUnconfirmed = true
							break
						}
					}

					// 2. Если есть неподтвержденные заявки - ЖЕЛТЫЙ
					if hasUnconfirmed {
						backgroundColor = &sheets.Color{
							Red:   1.0, // #FFEB9C
							Green: 0.92,
							Blue:  0.61,
						}
					} else {
						// 3. Если все заявки подтверждены - ЗЕЛЕНЫЙ
						backgroundColor = &sheets.Color{
							Red:   0.78, // #C6EFCE
							Green: 0.94,
							Blue:  0.81,
						}
					}
				}
			} else {
				// Нет активных заявок - СВОБОДНО (БЕЗ ЗАЛИВКИ)
				cellValue = "Свободно\n\nДоступно: " + fmt.Sprintf("%d/%d", item.TotalQuantity, item.TotalQuantity)
				// ЯВНО УСТАНАВЛИВАЕМ backgroundColor В NIL ДЛЯ ОТСУТСТВИЯ ЗАЛИВКИ
				backgroundColor = nil
			}

			rowData = append(rowData, cellValue)

			// Добавляем запрос на форматирование только если нужна заливка
			if backgroundColor != nil {
				formatRequests = append(formatRequests, &sheets.Request{
					RepeatCell: &sheets.RepeatCellRequest{
						Range: &sheets.GridRange{
							SheetId:          sheetId,
							StartRowIndex:    int64(rowIndex + 3),
							EndRowIndex:      int64(rowIndex + 4),
							StartColumnIndex: int64(colIndex + 1),
							EndColumnIndex:   int64(colIndex + 2),
						},
						Cell: &sheets.CellData{
							UserEnteredFormat: &sheets.CellFormat{
								BackgroundColor:   backgroundColor,
								VerticalAlignment: "TOP",
								WrapStrategy:      "WRAP",
							},
						},
						Fields: "userEnteredFormat(backgroundColor,verticalAlignment,wrapStrategy)",
					},
				})
			} else {
				// ДЛЯ ЯЧЕЕК БЕЗ ЗАЛИВКИ ЯВНО УСТАНАВЛИВАЕМ БЕЛЫЙ ЦВЕТ
				formatRequests = append(formatRequests, &sheets.Request{
					RepeatCell: &sheets.RepeatCellRequest{
						Range: &sheets.GridRange{
							SheetId:          sheetId,
							StartRowIndex:    int64(rowIndex + 3),
							EndRowIndex:      int64(rowIndex + 4),
							StartColumnIndex: int64(colIndex + 1),
							EndColumnIndex:   int64(colIndex + 2),
						},
						Cell: &sheets.CellData{
							UserEnteredFormat: &sheets.CellFormat{
								BackgroundColor: &sheets.Color{
									Red:   1.0, // Белый цвет
									Green: 1.0,
									Blue:  1.0,
								},
								VerticalAlignment: "TOP",
								WrapStrategy:      "WRAP",
							},
						},
						Fields: "userEnteredFormat(backgroundColor,verticalAlignment,wrapStrategy)",
					},
				})
			}

			currentDate = currentDate.AddDate(0, 0, 1)
		}
		data = append(data, rowData)
	}

	// Форматирование названий аппаратов
	if len(items) > 0 {
		formatRequests = append(formatRequests, &sheets.Request{
			RepeatCell: &sheets.RepeatCellRequest{
				Range: &sheets.GridRange{
					SheetId:          sheetId,
					StartRowIndex:    3,
					EndRowIndex:      int64(3 + len(items)),
					StartColumnIndex: 0,
					EndColumnIndex:   1,
				},
				Cell: &sheets.CellData{
					UserEnteredFormat: &sheets.CellFormat{
						TextFormat: &sheets.TextFormat{
							Bold: true,
						},
						BackgroundColor: &sheets.Color{
							Red:   0.89,
							Green: 0.94,
							Blue:  0.85,
						},
					},
				},
				Fields: "userEnteredFormat(backgroundColor,textFormat)",
			},
		})
	}

	// Записываем данные в лист
	rangeData := "Бронирования!A1"
	valueRange := &sheets.ValueRange{
		Values: data,
	}

	_, err = s.service.Spreadsheets.Values.Update(s.bookingsSheetID, rangeData, valueRange).
		ValueInputOption("RAW").
		Do()

	if err != nil {
		return fmt.Errorf("unable to update schedule sheet: %v", err)
	}

	// Применяем все форматирования
	if len(formatRequests) > 0 {
		batchUpdateRequest := &sheets.BatchUpdateSpreadsheetRequest{
			Requests: formatRequests,
		}

		_, err = s.service.Spreadsheets.BatchUpdate(s.bookingsSheetID, batchUpdateRequest).Do()
		if err != nil {
			return fmt.Errorf("unable to apply formatting: %v", err)
		}
	}

	// Настраиваем ширину колонок
	return s.adjustColumnWidths(sheetId, len(dateHeaders))
}

// filterActiveBookings фильтрует активные заявки (исключает отмененные)
func (s *SheetsService) filterActiveBookings(bookings []models.Booking) []models.Booking {
	var active []models.Booking
	for _, booking := range bookings {
		if booking.Status != "cancelled" {
			active = append(active, booking)
		}
	}
	return active
}

// adjustColumnWidths настраивает ширину колонок
func (s *SheetsService) adjustColumnWidths(sheetId int64, dateCount int) error {
	requests := []*sheets.Request{}

	// Колонка A - названия аппаратов (ширина 200px)
	requests = append(requests, &sheets.Request{
		UpdateDimensionProperties: &sheets.UpdateDimensionPropertiesRequest{
			Range: &sheets.DimensionRange{
				SheetId:    sheetId, // ИСПРАВЛЕНО
				Dimension:  "COLUMNS",
				StartIndex: 0,
				EndIndex:   1,
			},
			Properties: &sheets.DimensionProperties{
				PixelSize: 200,
			},
			Fields: "pixelSize",
		},
	})

	// Колонки с датами (ширина 150px)
	if dateCount > 0 {
		requests = append(requests, &sheets.Request{
			UpdateDimensionProperties: &sheets.UpdateDimensionPropertiesRequest{
				Range: &sheets.DimensionRange{
					SheetId:    sheetId, // ИСПРАВЛЕНО
					Dimension:  "COLUMNS",
					StartIndex: 1,
					EndIndex:   int64(1 + dateCount),
				},
				Properties: &sheets.DimensionProperties{
					PixelSize: 150,
				},
				Fields: "pixelSize",
			},
		})
	}

	if len(requests) > 0 {
		batchUpdateRequest := &sheets.BatchUpdateSpreadsheetRequest{
			Requests: requests,
		}

		_, err := s.service.Spreadsheets.BatchUpdate(s.bookingsSheetID, batchUpdateRequest).Do()
		return err
	}

	return nil
}

// GetSheetIdByName возвращает ID листа по его названию
func (s *SheetsService) GetSheetIdByName(spreadID, sheetName string) (int64, error) {
	spreadsheet, err := s.service.Spreadsheets.Get(spreadID).Do()
	if err != nil {
		return 0, fmt.Errorf("unable to get spreadsheet: %v", err)
	}

	for _, sheet := range spreadsheet.Sheets {
		if sheet.Properties.Title == sheetName {
			return sheet.Properties.SheetId, nil
		}
	}

	return 0, fmt.Errorf("sheet '%s' not found", sheetName)
}

// ReplaceBookingsSheet полностью перезаписывает лист с заявками
func (s *SheetsService) ReplaceBookingsSheet(bookings []*models.Booking) error {
	// Очищаем весь лист (кроме заголовков)
	clearRange := "Bookings!A2:Z" // Предполагая, что заголовки в строке 1
	clearReq := &sheets.ClearValuesRequest{}

	_, err := s.service.Spreadsheets.Values.Clear(s.bookingsSheetID, clearRange, clearReq).Do()
	if err != nil {
		return fmt.Errorf("failed to clear bookings sheet: %v", err)
	}

	// Подготавливаем данные для записи
	var values [][]interface{}
	for _, booking := range bookings {
		row := []interface{}{
			booking.ID,
			booking.UserID,
			booking.UserName,
			booking.Phone,
			booking.ItemName,
			booking.Date.Format("02.01.2006"),
			booking.Status,
			booking.Comment,
			booking.CreatedAt.Format("02.01.2006 15:04"),
			booking.UpdatedAt.Format("02.01.2006 15:04"),
		}
		values = append(values, row)
	}

	// Записываем все данные
	valueRange := &sheets.ValueRange{
		Values: values,
	}

	_, err = s.service.Spreadsheets.Values.Update(s.bookingsSheetID, "Bookings!A2", valueRange).
		ValueInputOption("RAW").Do()
	if err != nil {
		return fmt.Errorf("failed to update bookings sheet: %v", err)
	}

	return nil
}
