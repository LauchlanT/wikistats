package api

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
	"wikistats/pkg/database"
)

type Credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type Service struct {
	db         database.Executer
	tokenCache sync.Map
}

func NewService(db database.Executer) *Service {
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

func (s *Service) Login(w http.ResponseWriter, r *http.Request) {
	var creds Credentials
	if err := json.NewDecoder(r.Body).Decode(&creds); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if isValid := s.db.ValidateLogin(creds.Username, creds.Password); !isValid {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	// Generate new bearer token
	randBytes := make([]byte, 32)
	_, err := rand.Read(randBytes)
	if err != nil {
		log.Printf("Error generating random bytes: %v", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
	}
	token := base64.URLEncoding.EncodeToString(randBytes)
	s.tokenCache.Store(token, time.Now())
	w.Write([]byte(token))
}

func (s *Service) Logout(w http.ResponseWriter, r *http.Request) {
	authHeader := r.Header.Get("Authorization")
	token := strings.TrimPrefix(authHeader, "Bearer ")
	s.tokenCache.Delete(token)
	w.WriteHeader(http.StatusNoContent)
}
