//go:build unit

package producer

import (
	"context"
	"sync"
	"testing"

	"github.com/twmb/franz-go/pkg/kgo"
)

type mockRecordProducer struct {
	records []*kgo.Record
	mu      sync.Mutex
	flushed bool
}

func (m *mockRecordProducer) Produce(ctx context.Context, r *kgo.Record, promise func(*kgo.Record, error)) {
	m.mu.Lock()
	m.records = append(m.records, r)
	m.mu.Unlock()
	if promise != nil {
		promise(r, nil)
	}
}

func (m *mockRecordProducer) Flush(ctx context.Context) error {
	m.mu.Lock()
	m.flushed = true
	m.mu.Unlock()
	return nil
}

func (m *mockRecordProducer) getRecords() []*kgo.Record {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.records
}

func init() {
	registerProducerImplementation("mock", func(t *testing.T) (recordProducer, func() []*kgo.Record, func()) {
		mock := &mockRecordProducer{}
		return mock, mock.getRecords, func() {}
	})
}
