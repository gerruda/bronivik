# Bronivik GO - Бронировочный бот

Управление бронированиями через Telegram с интеграцией Google Sheets, PostgreSQL и Redis.

## Требования

- Go 1.20+
- PostgreSQL 12+ / SQLite3
- Redis 5+
- Google Cloud Platform аккаунт

## Конфигурация

Основные параметры (`configs/config.yaml`):

```yaml
app:
  name: "bronivik-go"
  environment: "staging"  # production/staging
  version: "1.0.0"

telegram:
  bot_token: ${BOT_TOKEN}  # Обязательная переменная
  debug: true  # Включить логирование дебага

database:
  path: "./data/bookings.db"  # SQLite по умолчанию
  postgres:  # Опционально для PostgreSQL
    host: "localhost"
    user: ${BOT_USER}
    password: ${BOT_PASSWORD}

google:
  credentials_file: ${GOOGLE_CREDENTIALS_FILE}  # Путь к JSON-ключу Google API
  bookings_spreadsheet_id: ${BOOKINGS_SPREADSHEET_ID}
```

## Переменные окружения (`.env`)

```bash
# Обязательные:
BOT_TOKEN=your_telegram_token
GOOGLE_CREDENTIALS_FILE=path/to/service-account.json

# Опциональные (для PostgreSQL):
BOT_USER=postgres_user
BOT_PASSWORD=secure_password
POSTGRES_DB=bot_db
```

## Запуск

```bash
# Установка зависимостей
go mod tidy

# Запуск с конфигом по умолчанию
go run main.go --config=configs/config.yaml

# Или с переменными окружения
export BOT_TOKEN=your_token && go run main.go
```

## Команды бота

### Пользовательские команды

`/start` - Начало работы, проверка статуса броней  
`/book [дата] [время]` - Создать новую бронь (пример: `/book 2023-12-31 20:00`)  
`/my_bookings` - Показать активные брони  
`/cancel_booking [ID]` - Отменить бронь  
`/help` - Справка по командам

### Административные команды

`/approve [ID]` - Подтвердить бронь  
`/ban_user [ID]` - Добавить в черный список  
`/export_bookings` - Экспорт броней в Google Sheets  
`/stats` - Статистика бронирований  
`/system_info` - Техническая информация сервиса

❗ *Административные команды доступны только пользователям из списка managers (configs/config.yaml)*

```markdown
**Пример сценария бронирования**:

```bash
1. Пользователь: /book 2024-01-15 19:30
2. Бот: Запрос подтверждения данных
3. Менеджер: /approve 12345
4. Бот: Уведомление об успешной брони
```

## Мониторинг

- Prometheus: `http://localhost:9090/metrics`
- Healthcheck: `http://localhost:8080/health`

## Основные функции

✅ Управление списком менеджеров (configs/config.yaml: `managers`)  
🚫 Черный список пользователей (configs/config.yaml: `blacklist`)  
📊 Интеграция с Google Sheets через сервисный аккаунт  

## Лицензия

[МПЛ 2.0](https://www.apache.org/licenses/LICENSE-2.0)

---

! Важно: перед деплоем в production:
1. Установите `environment: production`
2. Отключите `telegram.debug`
3. Настройте SSL для PostgreSQL
4. Обновите `managers_contacts`

При разработке использовать команду для запуска инфраструктуры
git pull
docker-compose down
docker-compose build --no-cache
docker-compose up
docker logs -f booking-bot

docker-compose -f ./docker/docker-compose.dev.yml up -d

docker-compose -f ./docker/docker-compose.dev.yml --env-file .env up -d
