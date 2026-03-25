package consumer

import (
	"context"
	"wikistats/internal/database"

	"github.com/twmb/franz-go/pkg/kgo"
)

type RPClient interface {
	PollFetches(context.Context) Fetcher
	CommitRecords(context.Context, ...*kgo.Record) error
}

type Fetcher interface {
	Errors() []kgo.FetchError
	Records() []*kgo.Record
}

// Need to wrap the kgo Client to use the Fetcher interface instead of kgo.Fetches
type RPClientWrapper struct {
	Client *kgo.Client
}

func (w *RPClientWrapper) PollFetches(ctx context.Context) Fetcher {
	return w.Client.PollFetches(ctx)
}

func (w *RPClientWrapper) CommitRecords(ctx context.Context, errs ...*kgo.Record) error {
	return w.Client.CommitRecords(ctx, errs...)
}

type Consumer interface {
	Consume(context.Context, database.Repository, RPClient) error
}
