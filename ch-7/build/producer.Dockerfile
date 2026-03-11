#Stage 1 app builder
FROM golang:1.25-alpine AS builder

WORKDIR /app

RUN adduser -u 1000 -D -H appuser

COPY go.mod go.sum ./
RUN go mod download

COPY cmd/producer /app
COPY internal /app/internal
COPY .env /app/.env

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o main main.go

#Stage 2 app container
FROM scratch AS container

#SSL certs are needed to connect to Wikimedia API, and scratch base images lack them
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /app/.env /.env
COPY --from=builder /app/main /main

COPY --from=builder /etc/passwd /etc/passwd
COPY --from=builder /etc/group /etc/group
USER 1000:1000

CMD [ "/main" ]