package database

import (
	"context"
	"sync"
	"time"

	"github.com/shaibearary/utxo_chat/message"
)

// MessageEntry stores a message with its timestamp
type MessageEntry struct {
	Data      []byte    // The full serialized message
	Timestamp time.Time // When the message was received
}

// MemoryDB is an in-memory implementation of the Database interface.
type MemoryDB struct {
	outpoints map[message.Outpoint]struct{}     // Track seen outpoints for rate limiting
	messages  map[message.Outpoint]MessageEntry // Store full messages
	mu        sync.RWMutex
}

// AddMessage implements Database.
func (db *MemoryDB) AddMessage(
	ctx context.Context, outpoint message.Outpoint, data []byte) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	db.mu.Lock()
	defer db.mu.Unlock()

	// Store the full message data with timestamp
	db.messages[outpoint] = MessageEntry{
		Data:      make([]byte, len(data)),
		Timestamp: time.Now(),
	}
	copy(db.messages[outpoint].Data, data)

	// Also mark outpoint as seen for rate limiting
	db.outpoints[outpoint] = struct{}{}

	return nil
}

// GetMessage implements Database.
func (db *MemoryDB) GetMessage(
	ctx context.Context, outpoint message.Outpoint) ([]byte, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	db.mu.RLock()
	defer db.mu.RUnlock()

	entry, exists := db.messages[outpoint]
	if !exists {
		return nil, nil // Message not found
	}

	// Return a copy of the data
	result := make([]byte, len(entry.Data))
	copy(result, entry.Data)
	return result, nil
}

// NewMemoryDB creates a new in-memory database.
func NewMemoryDB() *MemoryDB {
	return &MemoryDB{
		outpoints: make(map[message.Outpoint]struct{}),
		messages:  make(map[message.Outpoint]MessageEntry),
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
	delete(db.messages, outpoint) // Also remove the message data
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
		delete(db.messages, outpoint) // Also remove the message data
	}
	return nil
}

// GetAllOutpoints returns all known outpoints (for INV broadcasting)
func (db *MemoryDB) GetAllOutpoints(ctx context.Context) ([]message.Outpoint, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	db.mu.RLock()
	defer db.mu.RUnlock()

	outpoints := make([]message.Outpoint, 0, len(db.outpoints))
	for outpoint := range db.outpoints {
		outpoints = append(outpoints, outpoint)
	}
	return outpoints, nil
}

// ExpireMessages removes messages older than the given duration
func (db *MemoryDB) ExpireMessages(maxAge time.Duration) int {
	db.mu.Lock()
	defer db.mu.Unlock()

	cutoff := time.Now().Add(-maxAge)
	expiredCount := 0

	for outpoint, entry := range db.messages {
		if entry.Timestamp.Before(cutoff) {
			delete(db.messages, outpoint)
			delete(db.outpoints, outpoint)
			expiredCount++
		}
	}

	return expiredCount
}

// Close shuts down the database.
func (db *MemoryDB) Close() error {
	// Nothing to do for in-memory implementation
	return nil
}
