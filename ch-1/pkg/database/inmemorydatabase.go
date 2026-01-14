package database

import "sync"

type InMemoryDatabase struct {
	lock         sync.Mutex
	messageCount int
	users        map[string]struct{}
	bots         map[string]struct{}
	servers      map[string]struct{}
}

func NewInMemoryDatabase() *InMemoryDatabase {
	return &InMemoryDatabase{
		users:   make(map[string]struct{}),
		bots:    make(map[string]struct{}),
		servers: make(map[string]struct{}),
	}
}

func (d *InMemoryDatabase) UpdateDatabase(user string, server string, isBot bool) {
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

func (d *InMemoryDatabase) GetStats() (messages int, users int, bots int, servers int) {
	d.lock.Lock()
	defer d.lock.Unlock()

	return d.messageCount, len(d.users), len(d.bots), len(d.servers)
}
