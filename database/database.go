package database

import (
	"context"
)

// Database defines the interface for UTXOchat's database operations
type Database interface {
	// Close closes the database connection
	Close() error

	// HasOutpoint checks if an outpoint exists in the database
	HasOutpoint(ctx context.Context, outpoint Outpoint) (bool, error)

	// AddOutpoint adds an outpoint to the database
	AddOutpoint(ctx context.Context, outpoint Outpoint) error

	// AddMessage adds a message to the database
	AddMessage(ctx context.Context, outpoint Outpoint, data []byte) error

	// GetMessage retrieves a message from the database by outpoint
	GetMessage(ctx context.Context, outpoint Outpoint) ([]byte, error)
}

// Outpoint represents a Bitcoin transaction output
type Outpoint struct {
	TxID  [32]byte
	Index uint32
}
