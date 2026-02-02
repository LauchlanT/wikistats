package api

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

type AuthService struct {
	tokenLock  sync.RWMutex
	tokenCache map[string]time.Time
}

func (a *AuthService) SetToken() (*string, error) {
	randBytes := make([]byte, 32)
	_, err := rand.Read(randBytes)
	if err != nil {
		return nil, fmt.Errorf("generating random bytes: %w", err)
	}
	token := base64.URLEncoding.EncodeToString(randBytes)
	a.tokenLock.Lock()
	a.tokenCache[token] = time.Now()
	a.tokenLock.Unlock()
	return &token, nil
}

func (a *AuthService) DeleteToken(token string) error {
	a.tokenLock.Lock()
	delete(a.tokenCache, token)
	a.tokenLock.Unlock()
	return nil
}

func (a *AuthService) GetToken(token string) (*time.Time, error) {
	a.tokenLock.RLock()
	issuedAt, ok := a.tokenCache[token]
	a.tokenLock.RUnlock()
	if !ok {
		return nil, nil
	}
	return &issuedAt, nil
}

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
		issuedAt, err := s.auth.GetToken(token)
		if err != nil {
			log.Printf("Error getting token: %v", err)
			http.Error(w, "Internal error", http.StatusInternalServerError)
			return
		}
		if issuedAt == nil {
			http.Error(w, "Invalid token", http.StatusUnauthorized)
			return
		}

		// Validate that token has not expired
		if time.Since(*issuedAt) > 1*time.Hour {
			if err := s.auth.DeleteToken(token); err != nil {
				log.Printf("Error deleting token: %v", err)
				http.Error(w, "Internal error", http.StatusInternalServerError)
				return
			}
			http.Error(w, "Token expired", http.StatusUnauthorized)
			return
		}

		next(w, r)
	}
}
