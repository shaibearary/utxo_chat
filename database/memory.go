package database

import (
	"context"
	"sync"

	"github.com/shaibearary/utxo_chat/message"
)

// MemoryDB is an in-memory implementation of the Database interface.
type MemoryDB struct {
	outpoints map[message.Outpoint]struct{}
	mu        sync.RWMutex
}

// AddMessage implements Database.
func (db *MemoryDB) AddMessage(
	ctx context.Context, outpoint message.Outpoint, data []byte) error {
	panic("unimplemented")
}

// GetMessage implements Database.
func (db *MemoryDB) GetMessage(
	ctx context.Context, outpoint message.Outpoint) ([]byte, error) {
	panic("unimplemented")
}

// NewMemoryDB creates a new in-memory database.
func NewMemoryDB() *MemoryDB {
	return &MemoryDB{
		outpoints: make(map[message.Outpoint]struct{}),
	}
}

// HasOutpoint checks if the outpoint has been seen before.
func (db *MemoryDB) HasOutpoint(
	ctx context.Context, outpoint message.Outpoint) (bool, error) {
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	default:
	}

	db.mu.RLock()
	defer db.mu.RUnlock()

	_, exists := db.outpoints[outpoint]
	return exists, nil
}

// AddOutpoint adds an outpoint to the database.
func (db *MemoryDB) AddOutpoint(
	ctx context.Context, outpoint message.Outpoint) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	db.mu.Lock()
	defer db.mu.Unlock()

	db.outpoints[outpoint] = struct{}{}
	return nil
}

// RemoveOutpoint removes an outpoint from the database.
func (db *MemoryDB) RemoveOutpoint(
	ctx context.Context, outpoint message.Outpoint) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	db.mu.Lock()
	defer db.mu.Unlock()

	delete(db.outpoints, outpoint)
	return nil
}

// RemoveOutpoints removes multiple outpoints from the database.
func (db *MemoryDB) RemoveOutpoints(
	ctx context.Context, outpoints []message.Outpoint) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	db.mu.Lock()
	defer db.mu.Unlock()

	for _, outpoint := range outpoints {
		delete(db.outpoints, outpoint)
	}
	return nil
}

// Close shuts down the database.
func (db *MemoryDB) Close() error {
	// Nothing to do for in-memory implementation
	return nil
}
