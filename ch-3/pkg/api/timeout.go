package api

import (
	"context"
	"net/http"
	"time"
)

type TimeoutService struct {
	timeout time.Duration
}

func (s *Service) TimeoutMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), s.timeout.timeout)
		defer cancel()
		next(w, r.WithContext(ctx))
	}
}
