package api

import (
	"net/http"
	"strings"
	"time"
)

func (s *Service) AuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Read in bearer token
		authHeader := r.Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			http.Error(w, "Missing token", http.StatusUnauthorized)
			return
		}
		token := strings.TrimPrefix(authHeader, "Bearer ")

		// Validate token exists in cache from prior login
		val, ok := s.tokenCache.Load(token)
		if !ok {
			http.Error(w, "Invalid token", http.StatusUnauthorized)
			return
		}

		// Validate that token has not expired
		issuedAt := val.(time.Time)
		if time.Since(issuedAt) > 1*time.Hour {
			s.tokenCache.Delete(token)
			http.Error(w, "Token expired", http.StatusUnauthorized)
			return
		}

		next(w, r)
	}
}
