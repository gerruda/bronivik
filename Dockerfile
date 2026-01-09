FROM golang:1.24-alpine AS builder

WORKDIR /app

# Устанавливаем зависимости для сборки
#RUN #apk add --no-cache git ca-certificates
RUN apk add --no-cache git ca-certificates gcc musl-dev

# Копируем go.mod и go.sum
COPY go.mod go.sum ./
RUN go mod download

# Копируем весь проект
COPY . .

# Собираем приложение
RUN CGO_ENABLED=1 GOOS=linux go build -a -installsuffix cgo -o /go/bin/bot ./cmd/bot
RUN CGO_ENABLED=1 GOOS=linux go build -a -installsuffix cgo -o /go/bin/api ./cmd/api

# Финальный образ
FROM alpine:latest

#RUN #apk --no-cache add ca-certificates tzdata
RUN apk --no-cache add ca-certificates tzdata sqlite-libs

WORKDIR /app/

# Копируем бинарники из builder
COPY --from=builder /go/bin/bot .
COPY --from=builder /go/bin/api .

# Создаем папку для конфигов
RUN mkdir /configs

# Добавляем HEALTHCHECK
HEALTHCHECK --interval=30s --timeout=3s \
  CMD wget --spider http://localhost:8080/healthz || exit 1

# Указываем команду запуска
CMD ["./bot"]
