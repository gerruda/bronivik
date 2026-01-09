.PHONY: build run stop logs clean

build:
	docker-compose build

run:
	docker-compose up -d

stop:
	docker-compose down

logs:
	docker-compose logs -f

clean:
	docker-compose down -v
	docker system prune -f

migrate:
	# Для будущих миграций БД
	# docker-compose exec telegram-bot ./migrate

# Сборка без кэша
rebuild:
	docker-compose build --no-cache

test:
	go test -v -race ./...

test-coverage:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out

lint:
	golangci-lint run
