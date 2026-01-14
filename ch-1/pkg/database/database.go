package database

type Executer interface {
	UpdateDatabase(string, string, bool)
	GetStats() (int, int, int, int)
}
