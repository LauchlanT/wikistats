package consumer

import (
	"context"
	"wikistats/internal/database"

	"github.com/twmb/franz-go/pkg/kgo"
)

type RPClient interface {
	PollFetches(context.Context) Fetcher
	CommitRecords(context.Context, ...*kgo.Record) error
	ProduceSync(context.Context, ...*kgo.Record) kgo.ProduceResults
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

func (w *RPClientWrapper) CommitRecords(ctx context.Context, records ...*kgo.Record) error {
	return w.Client.CommitRecords(ctx, records...)
}

func (w *RPClientWrapper) ProduceSync(ctx context.Context, records ...*kgo.Record) kgo.ProduceResults {
	return w.Client.ProduceSync(ctx, records...)
}

type Consumer interface {
	Consume(context.Context, database.Repository, RPClient) error
}
