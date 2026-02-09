package api

import "net/http"

func NewRouter(s *Service) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthcheck", s.TimeoutMiddleware(s.Healthcheck))
	mux.HandleFunc("GET /stats", s.TimeoutMiddleware(s.AuthMiddleware(s.Stats)))
	mux.HandleFunc("POST /login", s.TimeoutMiddleware(s.Login))
	mux.HandleFunc("GET /logout", s.TimeoutMiddleware(s.AuthMiddleware(s.Logout)))
	return mux
}
