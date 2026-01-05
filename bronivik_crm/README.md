# Bronivik CRM Bot

Hourly booking system for cabinets, integrated with the Bronivik Jr core service.

## Overview

Bronivik CRM is a specialized Telegram bot designed for managing hourly bookings of cabinets. It provides a user-friendly interface for clients to check availability and book time slots, while allowing managers to oversee the booking process.

## Features

- **Hourly Booking**: Users can book cabinets for specific hours.
- **Availability Check**: Real-time availability checks via integration with Bronivik Jr API.
- **Calendar Interface**: Intuitive calendar-based date selection.
- **Manager Dashboard**: Special features for authorized managers to view and manage bookings.
- **Caching**: Redis-based caching for API responses to improve performance.
- **Monitoring**: Prometheus metrics and health check endpoints.
- **Persistence**: SQLite database for storing local booking data and user states.

## Architecture

The bot is built as a standalone Go service that communicates with the main Bronivik Jr API.

- **`cmd/bot`**: Entry point of the application.
- **`internal/api`**: HTTP client for interacting with Bronivik Jr.
- **`internal/bot`**: Core bot logic, handlers, and state management.
- **`internal/database`**: SQLite persistence layer.
- **`internal/models`**: Domain models (Cabinet, Booking, User).
- **`internal/metrics`**: Prometheus metrics implementation.

## Configuration

Configuration is managed via a YAML file. By default, the bot looks for `configs/config.yaml`, but you can override this using the `CRM_CONFIG_PATH` environment variable.

The config file supports `${ENV_VAR}` placeholders (values are expanded from the process environment at startup).

### Key Configuration Sections

- **`telegram`**: Bot token and debug settings.
- **`database`**: Path to the SQLite database file.
- **`redis`**: Connection details for caching.
- **`api`**: Base URL and API key for Bronivik Jr integration.
- **`booking`**: Rules for bookings (min advance time, max advance days, etc.).
- **`managers`**: List of Telegram User IDs with manager privileges.

Example configuration:

```yaml
telegram:
  bot_token: "YOUR_BOT_TOKEN"
api:
  base_url: "http://localhost:8080"
  api_key: "${CRM_API_KEY}"
  api_extra: "${CRM_API_EXTRA}"
```

## How to Run

### Local Development

1. Ensure you have Go 1.24+ installed.
2. Copy `configs/config.yaml` and fill in your settings.
3. Run the bot:

```bash
go run cmd/bot/main.go
```

### Docker

Build the image:

```bash
# Build from the repository root (shared go.mod/go.sum)
cd ..
docker build -f bronivik_crm/Dockerfile -t bronivik-crm .
```

Run the container:

```bash
docker run -e CRM_CONFIG_PATH=/app/configs/config.yaml -v $(pwd)/data:/app/data bronivik-crm
```

Alternatively, run everything via docker compose from the repository root:

```bash
cp .env.example .env
docker compose up -d --build
```

## API Integration

Bronivik CRM integrates with Bronivik Jr via its REST API:

- `GET /api/v1/items`: To list available cabinets.
- `GET /api/v1/availability/{item}`: To check availability for a specific date.
- `POST /api/v1/availability/bulk`: For bulk availability checks.

Authentication is handled via the `x-api-key` header.

If Bronivik Jr API auth is enabled, the CRM bot must send two headers:

- `x-api-key`
- `x-api-extra`

In docker compose, the default integration URL is:

- `http://grpc-api:8080`
