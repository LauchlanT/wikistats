FROM golang:1.25-alpine

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY e2e-testing /app
COPY internal /app/internal

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o main main.go

CMD [ "/app/main" ]