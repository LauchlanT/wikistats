#Stage 1 app builder
FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY cmd /app/cmd
COPY pkg /app/pkg

RUN go build -ldflags="-s -w" -o main cmd/main.go

CMD ["/bin/sh"]

#Stage 2 app container
FROM golang:1.25-alpine AS container

WORKDIR /app

COPY --from=builder /app/main /app/main

CMD [ "/app/main" ]