package database

type Executer interface {
	UpdateDatabase(messageID string, username string, servername string, isBot bool)
	GetStats() (messages int, users int, bots int, servers int)
	ValidateLogin(username string, password string) bool
}
