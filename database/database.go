package database

import (
	"context"
	"time"

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

	// RemoveOutpoint removes an outpoint from the database
	RemoveOutpoint(ctx context.Context, outpoint message.Outpoint) error

	// RemoveOutpoints removes multiple outpoints from the database
	RemoveOutpoints(ctx context.Context, outpoints []message.Outpoint) error

	// AddMessage adds a message to the database
	AddMessage(ctx context.Context, outpoint message.Outpoint, data []byte) error

	// GetMessage retrieves a message from the database by outpoint
	GetMessage(ctx context.Context, outpoint message.Outpoint) ([]byte, error)

	// GetAllOutpoints returns all known outpoints (for INV broadcasting)
	GetAllOutpoints(ctx context.Context) ([]message.Outpoint, error)

	// ExpireMessages removes messages older than the given duration
	ExpireMessages(maxAge time.Duration) int
}
