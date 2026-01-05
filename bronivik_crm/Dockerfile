FROM golang:1.24-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY bronivik_crm/ ./bronivik_crm/
RUN CGO_ENABLED=0 GOOS=linux go build -o /bronivik-crm ./bronivik_crm/cmd/bot

FROM alpine:latest
RUN apk --no-cache add ca-certificates tzdata
WORKDIR /app
COPY --from=builder /bronivik-crm .
COPY bronivik_crm/configs/ ./configs/

CMD ["./bronivik-crm", "--config=configs/config.yaml"]
