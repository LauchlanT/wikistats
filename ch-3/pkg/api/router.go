package api

import "net/http"

func NewRouter(s *Service) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthcheck", s.Healthcheck)
	mux.HandleFunc("/stats", s.AuthMiddleware(s.Stats))
	mux.HandleFunc("/login", s.Login)
	mux.HandleFunc("/logout", s.AuthMiddleware(s.Logout))
	return mux
}
