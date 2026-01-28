package database

import (
	"log"
	"sync"

	"golang.org/x/crypto/bcrypt"
)

type InMemoryDatabase struct {
	lock     sync.Mutex
	messages map[string]struct{}
	users    map[string]struct{}
	bots     map[string]struct{}
	servers  map[string]struct{}
	accounts map[string]string
}

func NewInMemoryDatabase() *InMemoryDatabase {
	db := &InMemoryDatabase{
		messages: make(map[string]struct{}),
		users:    make(map[string]struct{}),
		bots:     make(map[string]struct{}),
		servers:  make(map[string]struct{}),
		accounts: make(map[string]string),
	}
	hash, err := bcrypt.GenerateFromPassword([]byte("admin"), 14)
	if err != nil {
		log.Printf("Could not set password for admin account: %v", err)
	} else {
		db.accounts["admin"] = string(hash)
	}
	return db
}

func (d *InMemoryDatabase) UpdateDatabase(id string, user string, server string, isBot bool) {
	d.lock.Lock()
	defer d.lock.Unlock()

	d.messages[id] = struct{}{}
	if isBot {
		d.bots[user] = struct{}{}
	} else {
		d.users[user] = struct{}{}
	}
	d.servers[server] = struct{}{}
}

func (d *InMemoryDatabase) GetStats() (messages int, users int, bots int, servers int) {
	d.lock.Lock()
	defer d.lock.Unlock()

	return len(d.messages), len(d.users), len(d.bots), len(d.servers)
}

func (d *InMemoryDatabase) ValidateLogin(username string, password string) bool {
	d.lock.Lock()
	defer d.lock.Unlock()

	err := bcrypt.CompareHashAndPassword([]byte(d.accounts[username]), []byte(password))
	return err == nil
}
