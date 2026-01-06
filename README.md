# Bronivik GO

Комплексная система бронирования на языке Go. Состоит из двух взаимосвязанных ботов:

1. **Bronivik Jr** — основной сервис для бронирования оборудования на полный день.
2. **Bronivik CRM** — специализированный бот для почасового бронирования кабинетов с проверкой доступности оборудования через API основного сервиса.

Система интегрирована с Google Sheets, SQLite (WAL) и Redis.

---

## Архитектура и компоненты

### 1. Bronivik Jr (Core Service)

- **Telegram Bot**: Интерфейс для пользователей и менеджеров оборудования.
- **REST API & gRPC**: Точки интеграции для внешних сервисов (в т.ч. CRM бота).
- **SQLite (WAL)**: Основное хранилище данных (брони, пользователи, оборудование).
- **Google Sheets Worker**: Асинхронная очередь задач для синхронизации броней с таблицами Google.
- **Event Bus**: Внутренняя шина событий для разделения бизнес-логики и побочных эффектов.
- **Redis**: Кэширование состояний пользователей и защита от спама (Rate Limiting).

### 2. Bronivik CRM (Cabinet Booking)

- **Telegram Bot**: Интерфейс для почасового бронирования физических кабинетов.
- **Интеграция**: При бронировании кабинета бот автоматически проверяет доступность выбранного аппарата через API Bronivik Jr.
- **SQLite**: Локальная база данных для расписания кабинетов и почасовых броней.

---

## Требования

- Go 1.24+
- SQLite3
- Redis 7+ (опционально, но рекомендуется)
- Google Cloud Service Account (для синхронизации с Google Sheets)

---

## Конфигурация

### Основной сервис (`configs/config.yaml`)

```yaml
app:
  name: "bronivik-go"
  environment: "staging"
  version: "1.0.0"

telegram:
  bot_token: ${BOT_TOKEN}
  debug: true

database:
  path: "./data/bookings.db"

google:
  credentials_file: ${GOOGLE_CREDENTIALS_FILE}
  bookings_spreadsheet_id: ${BOOKINGS_SPREADSHEET_ID}

api:
  enabled: true
  grpc_port: 8081
  http:
    enabled: true
    port: 8080
  auth:
    enabled: true
    keys: ["KEY1", "KEY2"]
```

### Список оборудования (`configs/items.yaml`)

В этом файле настраивается список доступного оборудования, их количество и порядок отображения.

---

## Переменные окружения (`.env`)

Скопируйте шаблон и заполните его:

```bash
cp .env.example .env
```

Основные переменные:

- `BOT_TOKEN`: Токен основного бота.
- `CRM_BOT_TOKEN`: Токен CRM бота.
- `CRM_API_KEY`: Ключ авторизации для запросов CRM -> Jr.
- `GOOGLE_CREDENTIALS_FILE`: Путь к JSON-файлу сервисного аккаунта Google Cloud.

---

## Запуск

### Docker Compose (лучший способ)

```bash
docker compose up -d --build
```

Это запустит:

- `booking-bot`: Основной бот.
- `booking-api`: gRPC (8081) + HTTP (8080) API.
- `bronivik-crm-bot`: CRM бот (8090).
- `booking-redis`: Redis сервер.

### Локально

```bash
# Основной бот
go run ./cmd/bot --config=configs/config.yaml

# API сервис
go run ./cmd/api --config=configs/config.yaml

# CRM бот
go run ./bronivik_crm/cmd/bot --config=bronivik_crm/configs/config.yaml
```

---

## Команды ботов

### Bronivik Jr (Основной)

- `/start` — Начало работы.
- `/book` — Запустить мастер бронирования оборудования.
- `/my_bookings` — Список моих активных броней.
- `/cancel_booking <ID>` — Отмена брони.

**Менеджеры (Jr):**

- `/approve <ID>` — Подтвердить бронь.
- `/stats` — Статистика за период.
- `/export_bookings` — Ручная синхронизация с Google Sheets.

### Bronivik CRM

- `/start` — Начало работы.
- `/book` — Выбор кабинета, оборудования, даты и времени (слота).
- `/my_bookings` — Мои записи в кабинеты.
- `/cancel_booking <ID>` — Отмена записи.

**Менеджеры (CRM):**

- `/pending` — Просмотр заявок на подпись.
- `/today_schedule` — Расписание по кабинетам на сегодня.

---

## Мониторинг

- **Prometheus Metrics**: `http://localhost:9090/metrics`
- **Health Checks (Jr API)**: `http://localhost:8080/healthz`
- **Health Checks (CRM)**: `http://localhost:8090/healthz`

## Разработка

```bash
make test          # Запуск всех тестов
make test-coverage # Отчет о покрытии
make lint          # Запуск линтера (golangci-lint)
```

---

## Технические детали

### API Эндпоинты (REST)

- `GET /api/v1/items` — Список всего оборудования.
- `GET /api/v1/availability/{item_name}?date=YYYY-MM-DD` — Проверка наличия на дату.
- `POST /api/v1/availability/bulk` — Массовая проверка.

### Google Sheets Worker

Все изменения в БД (создание, отмена, подтверждение) генерируют события, которые обрабатываются асинхронным воркером. Это гарантирует, что медленные запросы к Google API не блокируют интерфейс Telegram.

### Базы данных

Система использует SQLite в режиме **WAL (Write-Ahead Logging)**, что позволяет боту и API одновременно работать с базой без блокировок.

## Лицензия

МПЛ 2.0
