package google

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
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
	rowCache        map[int64]int
	cacheMu         sync.RWMutex
	lastRefresh     time.Time
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

	service := &SheetsService{
		service:         srv,
		usersSheetID:    usersSheetID,
		bookingsSheetID: bookingsSheetID,
		rowCache:        make(map[int64]int),
	}

	// Warm up cache in background
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = service.WarmUpCache(ctx)
	}()

	// Periodic cache refresh
	go func() {
		ticker := time.NewTicker(time.Duration(models.SheetsCacheTTL) * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			_ = service.WarmUpCache(ctx)
			cancel()
		}
	}()

	return service, nil
}

// TestConnection проверяет подключение к таблице
func (s *SheetsService) TestConnection(ctx context.Context) error {
	// Пробуем прочитать первую ячейку таблицы пользователей
	_, err := s.service.Spreadsheets.Values.Get(s.usersSheetID, "Users!A1").Context(ctx).Do()
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
func (s *SheetsService) UpdateUsersSheet(ctx context.Context, users []*models.User) error {
	// Подготавливаем данные
	values := make([][]interface{}, 0, len(users)+1)

	// Заголовки
	headers := []interface{}{
		"ID", "Telegram ID", "Username", "First Name", "Last Name",
		"Phone", "Is Manager", "Is Blacklisted", "Language Code",
		"Last Activity", "Created At",
	}
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
		Context(ctx).
		Do()

	return err
}

// WarmUpCache populates the row index cache by reading the entire ID column.
func (s *SheetsService) WarmUpCache(ctx context.Context) error {
	resp, err := s.service.Spreadsheets.Values.Get(s.bookingsSheetID, "Bookings!A:A").Context(ctx).Do()
	if err != nil {
		return err
	}

	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	s.rowCache = make(map[int64]int)
	s.lastRefresh = time.Now()

	for i, row := range resp.Values {
		if len(row) == 0 {
			continue
		}
		var id int64
		switch v := row[0].(type) {
		case float64:
			id = int64(v)
		case string:
			_, _ = fmt.Sscanf(v, "%d", &id)
		}
		if id > 0 {
			s.rowCache[id] = i + 1
		}
	}
	return nil
}

// AppendBooking добавляет новое бронирование
func (s *SheetsService) AppendBooking(ctx context.Context, booking *models.Booking) error {
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

	resp, err := s.service.Spreadsheets.Values.Append(s.bookingsSheetID, rangeData, valueRange).
		ValueInputOption("RAW").
		InsertDataOption("INSERT_ROWS").
		Context(ctx).
		Do()

	if err == nil && resp != nil && resp.Updates != nil {
		// Parse row number from range like "Bookings!A10:J10"
		var rowIdx int
		if _, sErr := fmt.Sscanf(resp.Updates.UpdatedRange, "Bookings!A%d", &rowIdx); sErr == nil && rowIdx > 0 {
			s.setCachedRow(booking.ID, rowIdx)
		}
	}

	return err
}

// UpsertBooking updates an existing booking row or appends a new one if not found.
func (s *SheetsService) UpsertBooking(ctx context.Context, booking *models.Booking) error {
	if booking == nil {
		return fmt.Errorf("booking is nil")
	}

	rowIdx, err := s.FindBookingRow(ctx, booking.ID)
	if err != nil {
		if errors.Is(err, sqlErrNotFound) {
			return s.AppendBooking(ctx, booking)
		}
		return err
	}

	rangeData := fmt.Sprintf("Bookings!A%d:J%d", rowIdx, rowIdx)
	valueRange := &sheets.ValueRange{
		Values: [][]interface{}{bookingRowValues(booking)},
	}

	_, err = s.service.Spreadsheets.Values.Update(s.bookingsSheetID, rangeData, valueRange).
		ValueInputOption("RAW").
		Context(ctx).
		Do()
	return err
}

// DeleteBookingRow removes the row that corresponds to bookingID.
func (s *SheetsService) DeleteBookingRow(ctx context.Context, bookingID int64) error {
	rowIdx, err := s.FindBookingRow(ctx, bookingID)
	if err != nil {
		return err
	}

	rangeData := fmt.Sprintf("Bookings!A%d:J%d", rowIdx, rowIdx)
	_, err = s.service.Spreadsheets.Values.Clear(s.bookingsSheetID, rangeData, &sheets.ClearValuesRequest{}).
		Context(ctx).
		Do()
	if err == nil {
		s.deleteCacheRow(bookingID)
	}
	return err
}

// UpdateBookingStatus updates status (and UpdatedAt) for a booking row.
func (s *SheetsService) UpdateBookingStatus(ctx context.Context, bookingID int64, status string) error {
	rowIdx, err := s.FindBookingRow(ctx, bookingID)
	if err != nil {
		return err
	}

	now := time.Now().Format("2006-01-02 15:04:05")

	statusRange := fmt.Sprintf("Bookings!E%d:E%d", rowIdx, rowIdx)
	_, err = s.service.Spreadsheets.Values.Update(s.bookingsSheetID, statusRange, &sheets.ValueRange{
		Values: [][]interface{}{{status}},
	}).ValueInputOption("RAW").Context(ctx).Do()
	if err != nil {
		return err
	}

	updatedRange := fmt.Sprintf("Bookings!J%d:J%d", rowIdx, rowIdx)
	_, err = s.service.Spreadsheets.Values.Update(s.bookingsSheetID, updatedRange, &sheets.ValueRange{
		Values: [][]interface{}{{now}},
	}).ValueInputOption("RAW").Context(ctx).Do()
	return err
}

// FindBookingRow locates row index (1-based) for booking_id in column A with cache.
func (s *SheetsService) FindBookingRow(ctx context.Context, bookingID int64) (int, error) {
	if bookingID == 0 {
		return 0, fmt.Errorf("booking id is required")
	}

	if row, ok := s.getCachedRow(bookingID); ok {
		return row, nil
	}

	// Cache miss - do a full scan and update cache for EVERYTHING to prevent future misses
	resp, err := s.service.Spreadsheets.Values.Get(s.bookingsSheetID, "Bookings!A:A").Context(ctx).Do()
	if err != nil {
		return 0, err
	}

	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()

	foundRow := 0
	for i, row := range resp.Values {
		if len(row) == 0 {
			continue
		}
		var id int64
		switch v := row[0].(type) {
		case float64:
			id = int64(v)
		case string:
			_, _ = fmt.Sscanf(v, "%d", &id)
		}

		if id > 0 {
			rowIdx := i + 1
			s.rowCache[id] = rowIdx
			if id == bookingID {
				foundRow = rowIdx
			}
		}
	}

	if foundRow > 0 {
		return foundRow, nil
	}

	return 0, sqlErrNotFound
}

var sqlErrNotFound = errors.New("booking row not found")

func (s *SheetsService) getCachedRow(id int64) (int, bool) {
	s.cacheMu.RLock()
	defer s.cacheMu.RUnlock()
	row, ok := s.rowCache[id]
	return row, ok
}

func (s *SheetsService) setCachedRow(id int64, row int) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	s.rowCache[id] = row
}

func (s *SheetsService) deleteCacheRow(id int64) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	delete(s.rowCache, id)
}

// ClearCache clears the row index cache.
func (s *SheetsService) ClearCache() {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	s.rowCache = make(map[int64]int)
}

func bookingRowValues(booking *models.Booking) []interface{} {
	return []interface{}{
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
}

// UpdateBookingsSheet обновляет всю таблицу бронирований
func (s *SheetsService) UpdateBookingsSheet(ctx context.Context, bookings []*models.Booking) error {
	values := make([][]interface{}, 0, len(bookings)+1)

	// Заголовки
	headers := []interface{}{
		"ID", "User ID", "Item ID", "Date", "Status",
		"User Name", "User Phone", "Item Name", "Created At", "Updated At",
	}
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
		Context(ctx).
		Do()

	return err
}

// UpdateScheduleSheet обновляет лист с расписанием бронирований в формате таблицы
func (s *SheetsService) UpdateScheduleSheet(
	ctx context.Context,
	startDate, endDate time.Time,
	dailyBookings map[string][]*models.Booking,
	items []*models.Item,
) error {
	sheetId, err := s.GetSheetIdByName(ctx, s.bookingsSheetID, "Бронирования")
	if err != nil {
		return fmt.Errorf("unable to get sheet ID: %v", err)
	}

	if err := s.clearScheduleSheet(ctx); err != nil {
		return err
	}

	days := int(endDate.Sub(startDate).Hours()/24) + 1
	if days <= 0 {
		return fmt.Errorf("invalid date range: startDate %s, endDate %s", startDate, endDate)
	}

	data := make([][]interface{}, 0, len(items)+5)
	var formatRequests []*sheets.Request

	// 1. Заголовок периода
	data = append(data, []interface{}{
		fmt.Sprintf("Период: %s - %s", startDate.Format("02.01.2006"), endDate.Format("02.01.2006")),
	})
	formatRequests = append(formatRequests, s.getPeriodHeaderFormat(sheetId))

	// 2. Пустая строка
	data = append(data, []interface{}{})

	// 3. Заголовки дат
	headerRow, dateCols := s.prepareDateHeaders(startDate, endDate)
	data = append(data, headerRow)
	if dateCols > 0 {
		formatRequests = append(formatRequests, s.getDateHeadersFormat(sheetId, int64(len(headerRow))))
	}

	// 4. Данные по аппаратам
	for rowIndex, item := range items {
		rowData, cellFormats := s.prepareItemRowData(item, startDate, dateCols, dailyBookings)
		data = append(data, rowData)

		for colIndex, cellFormat := range cellFormats {
			formatRequests = append(formatRequests, &sheets.Request{
				RepeatCell: &sheets.RepeatCellRequest{
					Range: &sheets.GridRange{
						SheetId:          sheetId,
						StartRowIndex:    int64(rowIndex + 3),
						EndRowIndex:      int64(rowIndex + 4),
						StartColumnIndex: int64(colIndex + 1),
						EndColumnIndex:   int64(colIndex + 2),
					},
					Cell:   cellFormat,
					Fields: "userEnteredFormat(backgroundColor,verticalAlignment,wrapStrategy)",
				},
			})
		}
	}

	if len(items) == 0 {
		data = append(data, s.prepareEmptyItemsRow(dateCols))
	} else {
		formatRequests = append(formatRequests, s.getItemNamesFormat(sheetId, len(items)))
	}

	if err := s.writeScheduleData(ctx, data); err != nil {
		return err
	}

	if len(formatRequests) > 0 {
		if err := s.applyBatchUpdate(ctx, formatRequests); err != nil {
			return err
		}
	}

	return s.adjustColumnWidths(sheetId, dateCols)
}

func (s *SheetsService) clearScheduleSheet(ctx context.Context) error {
	clearRange := "Бронирования!A:Z"
	_, err := s.service.Spreadsheets.Values.Clear(s.bookingsSheetID, clearRange, &sheets.ClearValuesRequest{}).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("unable to clear sheet: %v", err)
	}
	return nil
}

func (s *SheetsService) getPeriodHeaderFormat(sheetId int64) *sheets.Request {
	return &sheets.Request{
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
	}
}

func (s *SheetsService) prepareDateHeaders(startDate, endDate time.Time) (headerRow []interface{}, dateCols int) {
	headerRow = []interface{}{""}
	currentDate := startDate
	dateCols = 0
	for !currentDate.After(endDate) && dateCols < 100 {
		headerRow = append(headerRow, currentDate.Format("02.01"))
		dateCols++
		currentDate = currentDate.AddDate(0, 0, 1)
	}
	if len(headerRow) <= 1 {
		headerRow = append(headerRow, "Нет данных")
		dateCols = 1
	}
	return headerRow, dateCols
}

func (s *SheetsService) getDateHeadersFormat(sheetId, colCount int64) *sheets.Request {
	return &sheets.Request{
		RepeatCell: &sheets.RepeatCellRequest{
			Range: &sheets.GridRange{
				SheetId:          sheetId,
				StartRowIndex:    2,
				EndRowIndex:      3,
				StartColumnIndex: 1,
				EndColumnIndex:   colCount,
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
	}
}

func (s *SheetsService) prepareItemRowData(
	item *models.Item,
	startDate time.Time,
	dateCols int,
	dailyBookings map[string][]*models.Booking,
) ([]interface{}, []*sheets.CellData) {
	rowData := []interface{}{fmt.Sprintf("%s (%d)", item.Name, item.TotalQuantity)}
	cellFormats := make([]*sheets.CellData, 0, dateCols)

	currentDate := startDate
	for colIndex := 0; colIndex < dateCols; colIndex++ {
		dateKey := currentDate.Format("2006-01-02")
		bookings := dailyBookings[dateKey]

		var itemBookings []*models.Booking
		for i := range bookings {
			b := bookings[i]
			if b.ItemID == item.ID {
				itemBookings = append(itemBookings, b)
			}
		}

		cellValue, bgColor := s.formatScheduleCell(item, itemBookings)
		rowData = append(rowData, cellValue)

		cellFormats = append(cellFormats, &sheets.CellData{
			UserEnteredFormat: &sheets.CellFormat{
				VerticalAlignment: "TOP",
				WrapStrategy:      "WRAP",
				BackgroundColor:   bgColor,
			},
		})
		currentDate = currentDate.AddDate(0, 0, 1)
	}
	return rowData, cellFormats
}

func (s *SheetsService) formatScheduleCell(item *models.Item, itemBookings []*models.Booking) (string, *sheets.Color) {
	activeBookings := s.filterActiveBookings(itemBookings)
	bookedCount := len(activeBookings)

	if bookedCount == 0 {
		return "Свободно\n\nДоступно: " + fmt.Sprintf("%d/%d", item.TotalQuantity, item.TotalQuantity), &sheets.Color{Red: 1, Green: 1, Blue: 1}
	}

	var cellValue string
	hasUnconfirmed := false
	for _, b := range activeBookings {
		statusIcon := "❓"
		switch b.Status {
		case models.StatusConfirmed, models.StatusCompleted:
			statusIcon = "✅"
		case models.StatusPending, models.StatusChanged:
			statusIcon = "⏳"
			hasUnconfirmed = true
		case models.StatusCanceled:
			statusIcon = "❌"
		}

		cellValue += fmt.Sprintf("[№%d] %s %s (%s)\n", b.ID, statusIcon, b.UserName, b.Phone)
		if b.Comment != "" {
			cellValue += fmt.Sprintf("   💬 %s\n", b.Comment)
		}
	}
	cellValue += fmt.Sprintf("\nЗанято: %d/%d", bookedCount, item.TotalQuantity)

	var bgColor *sheets.Color
	if bookedCount >= int(item.TotalQuantity) {
		if hasUnconfirmed {
			bgColor = &sheets.Color{Red: 1.0, Green: 0.92, Blue: 0.61} // Yellow
		} else {
			bgColor = &sheets.Color{Red: 1.0, Green: 0.78, Blue: 0.81} // Red
		}
	} else {
		if hasUnconfirmed {
			bgColor = &sheets.Color{Red: 1.0, Green: 0.92, Blue: 0.61} // Yellow
		} else {
			bgColor = &sheets.Color{Red: 0.78, Green: 0.94, Blue: 0.81} // Green
		}
	}

	return cellValue, bgColor
}

func (s *SheetsService) prepareEmptyItemsRow(dateCols int) []interface{} {
	rowData := []interface{}{"Нет доступных аппаратов"}
	for i := 0; i < dateCols; i++ {
		rowData = append(rowData, "")
	}
	return rowData
}

func (s *SheetsService) getItemNamesFormat(sheetId int64, itemCount int) *sheets.Request {
	return &sheets.Request{
		RepeatCell: &sheets.RepeatCellRequest{
			Range: &sheets.GridRange{
				SheetId:          sheetId,
				StartRowIndex:    3,
				EndRowIndex:      int64(3 + itemCount),
				StartColumnIndex: 0,
				EndColumnIndex:   1,
			},
			Cell: &sheets.CellData{
				UserEnteredFormat: &sheets.CellFormat{
					TextFormat:      &sheets.TextFormat{Bold: true},
					BackgroundColor: &sheets.Color{Red: 0.89, Green: 0.94, Blue: 0.85},
				},
			},
			Fields: "userEnteredFormat(backgroundColor,textFormat)",
		},
	}
}

func (s *SheetsService) writeScheduleData(ctx context.Context, data [][]interface{}) error {
	valueRange := &sheets.ValueRange{Values: data}
	_, err := s.service.Spreadsheets.Values.Update(s.bookingsSheetID, "Бронирования!A1", valueRange).
		ValueInputOption("RAW").Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("unable to update schedule data: %v", err)
	}
	return nil
}

func (s *SheetsService) applyBatchUpdate(ctx context.Context, requests []*sheets.Request) error {
	batchUpdateRequest := &sheets.BatchUpdateSpreadsheetRequest{Requests: requests}
	_, err := s.service.Spreadsheets.BatchUpdate(s.bookingsSheetID, batchUpdateRequest).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("unable to apply batch update: %v", err)
	}
	return nil
}

// adjustColumnWidths настраивает ширину колонок
func (s *SheetsService) adjustColumnWidths(sheetId int64, dateCols int) error {
	if dateCols <= 0 {
		dateCols = 1 // Минимум одна колонка
	}

	var requests []*sheets.Request

	// Ширина для названий аппаратов
	requests = append(requests, &sheets.Request{
		UpdateDimensionProperties: &sheets.UpdateDimensionPropertiesRequest{
			Range: &sheets.DimensionRange{
				SheetId:    sheetId,
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

	// Ширина для колонок с датами
	for i := 1; i <= dateCols && i < 100; i++ { // Ограничим 100 колонками
		requests = append(requests, &sheets.Request{
			UpdateDimensionProperties: &sheets.UpdateDimensionPropertiesRequest{
				Range: &sheets.DimensionRange{
					SheetId:    sheetId,
					Dimension:  "COLUMNS",
					StartIndex: int64(i),
					EndIndex:   int64(i + 1),
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
		if err != nil {
			return fmt.Errorf("unable to adjust column widths: %v", err)
		}
	}

	return nil
}

// GetSheetIdByName возвращает ID листа по его названию
func (s *SheetsService) GetSheetIdByName(ctx context.Context, spreadID, sheetName string) (int64, error) {
	spreadsheet, err := s.service.Spreadsheets.Get(spreadID).Context(ctx).Do()
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
func (s *SheetsService) ReplaceBookingsSheet(ctx context.Context, bookings []*models.Booking) error {
	// Очищаем весь лист (кроме заголовков)
	clearRange := "Bookings!A2:Z" // Предполагая, что заголовки в строке 1
	clearReq := &sheets.ClearValuesRequest{}

	_, err := s.service.Spreadsheets.Values.Clear(s.bookingsSheetID, clearRange, clearReq).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("failed to clear bookings sheet: %v", err)
	}

	// Подготавливаем данные для записи
	values := make([][]interface{}, 0, len(bookings))
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
		ValueInputOption("RAW").Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("failed to update bookings sheet: %v", err)
	}

	// Re-populate cache
	s.cacheMu.Lock()
	s.rowCache = make(map[int64]int)
	for i, b := range bookings {
		s.rowCache[b.ID] = i + 2 // +2 because data starts at row 2
	}
	s.cacheMu.Unlock()

	return nil
}

// filterActiveBookings фильтрует активные заявки (исключает отмененные)
func (s *SheetsService) filterActiveBookings(bookings []*models.Booking) []*models.Booking {
	active := make([]*models.Booking, 0, len(bookings))
	for i := range bookings {
		booking := bookings[i]
		if booking.Status != models.StatusCanceled {
			active = append(active, booking)
		}
	}
	return active
}
