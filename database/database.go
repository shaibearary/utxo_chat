package database

import (
	"context"

	"github.com/shaibearary/utxo_chat/message"
)

// Database defines the interface for UTXOchat's database operations
type Database interface {
	// Close closes the database connection
	Close() error

	// HasOutpoint checks if an outpoint exists in the database
	HasOutpoint(ctx context.Context, outpoint message.Outpoint) (bool, error)

	// AddOutpoint adds an outpoint to the database
	AddOutpoint(ctx context.Context, outpoint message.Outpoint) error

	// AddMessage adds a message to the database
	AddMessage(ctx context.Context, outpoint message.Outpoint, data []byte) error

	// GetMessage retrieves a message from the database by outpoint
	GetMessage(ctx context.Context, outpoint message.Outpoint) ([]byte, error)
}
