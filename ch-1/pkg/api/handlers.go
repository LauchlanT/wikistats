package api

import (
	"fmt"
	"net/http"
	"wikistats/pkg/database"
)

type Service struct {
	db database.Database
}

func NewService(db database.Database) *Service {
	return &Service{
		db: db,
	}
}

func (s *Service) Healthcheck(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("Service active"))
}

func (s *Service) Stats(w http.ResponseWriter, r *http.Request) {
	messages, users, bots, servers := s.db.GetStats()
	stats := fmt.Sprintf("%d messages\n%d users\n%d bots\n%d servers", messages, users, bots, servers)
	w.Write([]byte(stats))
}
