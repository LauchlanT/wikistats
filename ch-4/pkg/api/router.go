package api

import "net/http"

func NewRouter(s *Service) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthcheck", s.Healthcheck)
	mux.HandleFunc("GET /stats", s.AuthMiddleware(s.Stats))
	mux.HandleFunc("POST /login", s.Login)
	mux.HandleFunc("GET /logout", s.AuthMiddleware(s.Logout))
	return mux
}
