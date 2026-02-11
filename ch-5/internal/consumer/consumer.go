package consumer

import (
	"context"
	"io"
	"wikistats/internal/database"
)

type Consumer interface {
	Connect(context.Context) (io.Reader, error)
	Consume(context.Context, io.Reader, database.Repository) error
}
