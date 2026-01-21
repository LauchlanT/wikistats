package database

import "sync"

type InMemoryDatabase struct {
	lock     sync.Mutex
	messages map[string]struct{}
	users    map[string]struct{}
	bots     map[string]struct{}
	servers  map[string]struct{}
}

func NewInMemoryDatabase() *InMemoryDatabase {
	return &InMemoryDatabase{
		messages: make(map[string]struct{}),
		users:    make(map[string]struct{}),
		bots:     make(map[string]struct{}),
		servers:  make(map[string]struct{}),
	}
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
