package api

import "net/http"

func NewRouter(s *Service) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthcheck", s.Healthcheck)
	mux.HandleFunc("/stats", s.Stats)
	return mux
}
