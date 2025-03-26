package database

import (
	"context"

	"github.com/btcsuite/btcd/chaincfg/chainhash"
)

// Outpoint represents a Bitcoin transaction output.
type Outpoint struct {
	TxID  chainhash.Hash
	Index uint32
}

// Database defines the interface for the UTXOchat database.
type Database interface {
	// HasOutpoint checks if the outpoint has been seen before.
	HasOutpoint(context.Context, Outpoint) (bool, error)

	// AddOutpoint adds an outpoint to the database.
	AddOutpoint(context.Context, Outpoint) error

	// RemoveOutpoint removes an outpoint from the database.
	RemoveOutpoint(context.Context, Outpoint) error

	// RemoveOutpoints removes multiple outpoints from the database.
	RemoveOutpoints(context.Context, []Outpoint) error

	// Close shuts down the database.
	Close() error
}
