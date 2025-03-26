package database

import (
	"fmt"
)

// Type represents the type of database.
type Type string

const (
	// TypeMemory is an in-memory database.
	TypeMemory Type = "memory"
	// TypeLevelDB is a LevelDB database.
	TypeLevelDB Type = "leveldb"
)

// Config defines the configuration for the database.
type Config struct {
	// Type is the type of database to use.
	Type Type
	// Path is the path to the database file.
	Path string
}

// New creates a new database based on the configuration.
func New(cfg Config) (Database, error) {
	switch cfg.Type {
	case TypeMemory:
		return NewMemoryDB(), nil
	case TypeLevelDB:
		// TODO: Implement LevelDB
		return nil, fmt.Errorf("leveldb not implemented yet")
	default:
		return nil, fmt.Errorf("unknown database type: %s", cfg.Type)
	}
}
