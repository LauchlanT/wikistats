package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
	"wikistats/pkg/config"
	"wikistats/pkg/database"
)

type Credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type TokenResponse struct {
	Token string `json:"token"`
}

type StatsResponse struct {
	Messages int `json:"messages"`
	Users    int `json:"users"`
	Bots     int `json:"bots"`
	Servers  int `json:"servers"`
}

type Service struct {
	db      database.Repository
	auth    *AuthService
	timeout *TimeoutService
}

func NewService(cfg config.APIConfig, db database.Repository) *Service {
	auth := &AuthService{
		tokenCache:  make(map[string]time.Time),
		tokenExpiry: cfg.TokenExpiry,
	}
	timeout := &TimeoutService{
		timeout: cfg.WorkerTimeout,
	}
	return &Service{
		db:      db,
		auth:    auth,
		timeout: timeout,
	}
}

func (s *Service) Healthcheck(w http.ResponseWriter, r *http.Request) {
	if _, err := w.Write([]byte("Service active")); err != nil {
		log.Printf("Error responding to healthcheck request: %v", err)
	}
}

func (s *Service) Stats(w http.ResponseWriter, r *http.Request) {
	stats, err := s.db.GetStats(r.Context())
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			http.Error(w, "Database timeout", http.StatusGatewayTimeout)
			return
		}
		if errors.Is(err, context.Canceled) {
			// Client disconnected, no need to respond
			return
		}
		log.Printf("Error getting stats: %v", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	statsResponse := &StatsResponse{Messages: stats.Messages, Users: stats.Users, Bots: stats.Bots, Servers: stats.Servers}
	if err := json.NewEncoder(w).Encode(&statsResponse); err != nil {
		log.Printf("Error responding to stats request: %v", err)
	}
}

func (s *Service) Login(w http.ResponseWriter, r *http.Request) {
	var creds Credentials
	body := io.LimitReader(r.Body, 2048)
	if err := json.NewDecoder(body).Decode(&creds); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if err := s.db.ValidateLogin(r.Context(), creds.Username, creds.Password); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			http.Error(w, "Database timeout", http.StatusGatewayTimeout)
			return
		}
		if errors.Is(err, context.Canceled) {
			// Client disconnected, no need to respond
			return
		}
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	token, err := s.auth.SetToken()
	if err != nil {
		log.Printf("Error generating bearer token: %v", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	tokenResponse := &TokenResponse{Token: *token}
	if err := json.NewEncoder(w).Encode(&tokenResponse); err != nil {
		log.Printf("Error responding to login request: %v", err)
	}
}

func (s *Service) Logout(w http.ResponseWriter, r *http.Request) {
	authHeader := r.Header.Get("Authorization")
	token := strings.TrimPrefix(authHeader, "Bearer ")
	if err := s.auth.DeleteToken(token); err != nil {
		log.Printf("Error deleting token from cache: %v", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
