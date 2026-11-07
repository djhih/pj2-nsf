package db

import "fmt"

var (
	NSF_DB_PATH = "/tmp/nsf.db"
)

type DbIf interface {
	Release() error

	Save(key, value string) error
	SaveAll(data map[string]string) error
	Exist(key string) (bool, error)
	Load(key string) (string, error)
	LoadAll() (map[string]string, error)
	Remove(key string) error
	RemoveAll() error
}

func NewDb(dbType string) (DbIf, error) {
	switch dbType {
	case "bbolt":
		return newBboltDb(NSF_DB_PATH)
	case "mongodb":
		return newMongoDb(mongoDbUri)
	}
	return nil, fmt.Errorf("unsupported db type: %s", dbType)
}
