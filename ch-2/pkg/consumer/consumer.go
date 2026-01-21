package consumer

import (
	"io"
	"wikistats/pkg/database"
)

type Consumer interface {
	Connect() (io.Reader, error)
	Consume(io.Reader, database.Executer) error
}
