package producer

import (
	"context"
	"io"

	"github.com/twmb/franz-go/pkg/kgo"
)

type recordProducer interface {
	Produce(context.Context, *kgo.Record, func(*kgo.Record, error))
	Flush(context.Context) error
}

type Consumer interface {
	Connect(context.Context) (io.Reader, error)
	Produce(context.Context, io.Reader, recordProducer) error
}
