package database

import (
	"context"
	"fmt"
	"sync"
)

// MemoryDB is an in-memory implementation of the Database interface.
type MemoryDB struct {
	outpoints map[string]struct{}
	mu        sync.RWMutex
}

// NewMemoryDB creates a new in-memory database.
func NewMemoryDB() *MemoryDB {
	return &MemoryDB{
		outpoints: make(map[string]struct{}),
	}
}

// outpointKey generates a unique key for an outpoint.
func outpointKey(outpoint Outpoint) string {
	return fmt.Sprintf("%s:%d", outpoint.TxID.String(), outpoint.Index)
}

// HasOutpoint checks if the outpoint has been seen before.
func (db *MemoryDB) HasOutpoint(ctx context.Context, outpoint Outpoint) (bool, error) {
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	default:
	}

	db.mu.RLock()
	defer db.mu.RUnlock()

	_, exists := db.outpoints[outpointKey(outpoint)]
	return exists, nil
}

// AddOutpoint adds an outpoint to the database.
func (db *MemoryDB) AddOutpoint(ctx context.Context, outpoint Outpoint) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	db.mu.Lock()
	defer db.mu.Unlock()

	db.outpoints[outpointKey(outpoint)] = struct{}{}
	return nil
}

// RemoveOutpoint removes an outpoint from the database.
func (db *MemoryDB) RemoveOutpoint(ctx context.Context, outpoint Outpoint) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	db.mu.Lock()
	defer db.mu.Unlock()

	delete(db.outpoints, outpointKey(outpoint))
	return nil
}

// RemoveOutpoints removes multiple outpoints from the database.
func (db *MemoryDB) RemoveOutpoints(ctx context.Context, outpoints []Outpoint) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	db.mu.Lock()
	defer db.mu.Unlock()

	for _, outpoint := range outpoints {
		delete(db.outpoints, outpointKey(outpoint))
	}
	return nil
}

// Close shuts down the database.
func (db *MemoryDB) Close() error {
	// Nothing to do for in-memory implementation
	return nil
}
