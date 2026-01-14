package database

import "sync"

type Database interface {
	UpdateDatabase(string, string, bool)
	GetStats() (int, int, int, int)
}

type MemoryDatabase struct {
	lock         sync.Mutex
	messageCount int
	users        map[string]struct{}
	bots         map[string]struct{}
	servers      map[string]struct{}
}

func NewDatabase() *MemoryDatabase {
	return &MemoryDatabase{
		users:   make(map[string]struct{}),
		bots:    make(map[string]struct{}),
		servers: make(map[string]struct{}),
	}
}

func (d *MemoryDatabase) UpdateDatabase(user string, server string, isBot bool) {
	d.lock.Lock()
	defer d.lock.Unlock()

	d.messageCount += 1
	if isBot {
		d.bots[user] = struct{}{}
	} else {
		d.users[user] = struct{}{}
	}
	d.servers[server] = struct{}{}
}

func (d *MemoryDatabase) GetStats() (messages int, users int, bots int, servers int) {
	d.lock.Lock()
	defer d.lock.Unlock()

	return d.messageCount, len(d.users), len(d.bots), len(d.servers)
}
