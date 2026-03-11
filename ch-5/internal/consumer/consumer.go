package consumer

import (
	"context"
	"wikistats/internal/database"

	"github.com/twmb/franz-go/pkg/kgo"
)

type Consumer interface {
	Consume(context.Context, database.Repository, *kgo.Client) error
}
