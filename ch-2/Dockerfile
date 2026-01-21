#Stage 1 app builder
FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY cmd /app/cmd
COPY pkg /app/pkg
COPY .env /app/.env

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o main cmd/main.go

#Stage 2 app container
FROM scratch AS container

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /app/.env /.env
COPY --from=builder /app/main /main

CMD [ "/main" ]