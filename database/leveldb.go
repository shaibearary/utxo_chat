package database

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/shaibearary/utxo_chat/message"
	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/util"
)

// LevelDB implements the Database interface using LevelDB for persistent storage
type LevelDB struct {
	db *leveldb.DB
}

// NewLevelDB creates a new LevelDB instance
func NewLevelDB(path string) (*LevelDB, error) {
	db, err := leveldb.OpenFile(path, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to open leveldb at %s: %v", path, err)
	}

	return &LevelDB{
		db: db,
	}, nil
}

// Close closes the LevelDB connection
func (db *LevelDB) Close() error {
	return db.db.Close()
}

// HasOutpoint checks if an outpoint exists in the database
func (db *LevelDB) HasOutpoint(ctx context.Context, outpoint message.Outpoint) (bool, error) {
	key := makeOutpointKey(outpoint)
	exists, err := db.db.Has(key, nil)
	if err != nil {
		return false, fmt.Errorf("failed to check outpoint: %v", err)
	}
	return exists, nil
}

// AddOutpoint adds an outpoint to the database (for rate limiting)
func (db *LevelDB) AddOutpoint(ctx context.Context, outpoint message.Outpoint) error {
	key := makeOutpointKey(outpoint)
	timestamp := make([]byte, 8)
	binary.LittleEndian.PutUint64(timestamp, uint64(time.Now().Unix()))

	return db.db.Put(key, timestamp, nil)
}

// RemoveOutpoint removes an outpoint from the database
func (db *LevelDB) RemoveOutpoint(ctx context.Context, outpoint message.Outpoint) error {
	key := makeOutpointKey(outpoint)

	// Remove both the outpoint and the message
	err := db.db.Delete(key, nil)
	if err != nil {
		return fmt.Errorf("failed to remove outpoint: %v", err)
	}

	msgKey := makeMessageKey(outpoint)
	err = db.db.Delete(msgKey, nil)
	if err != nil && err != leveldb.ErrNotFound {
		return fmt.Errorf("failed to remove message: %v", err)
	}

	return nil
}

// RemoveOutpoints removes multiple outpoints from the database
func (db *LevelDB) RemoveOutpoints(ctx context.Context, outpoints []message.Outpoint) error {
	batch := new(leveldb.Batch)

	for _, outpoint := range outpoints {
		outpointKey := makeOutpointKey(outpoint)
		messageKey := makeMessageKey(outpoint)

		batch.Delete(outpointKey)
		batch.Delete(messageKey)
	}

	return db.db.Write(batch, nil)
}

// AddMessage adds a message to the database
func (db *LevelDB) AddMessage(ctx context.Context, outpoint message.Outpoint, data []byte) error {
	// Store the message data
	msgKey := makeMessageKey(outpoint)
	entry := MessageEntry{
		Data:      data,
		Timestamp: time.Now(),
	}

	entryData, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to marshal message entry: %v", err)
	}

	err = db.db.Put(msgKey, entryData, nil)
	if err != nil {
		return fmt.Errorf("failed to store message: %v", err)
	}

	// Also store the outpoint for rate limiting
	return db.AddOutpoint(ctx, outpoint)
}

// GetMessage retrieves a message from the database
func (db *LevelDB) GetMessage(ctx context.Context, outpoint message.Outpoint) ([]byte, error) {
	key := makeMessageKey(outpoint)

	entryData, err := db.db.Get(key, nil)
	if err != nil {
		if err == leveldb.ErrNotFound {
			return nil, nil // Message not found
		}
		return nil, fmt.Errorf("failed to get message: %v", err)
	}

	var entry MessageEntry
	err = json.Unmarshal(entryData, &entry)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal message entry: %v", err)
	}

	return entry.Data, nil
}

// GetAllOutpoints returns all known outpoints (for INV broadcasting)
func (db *LevelDB) GetAllOutpoints(ctx context.Context) ([]message.Outpoint, error) {
	var outpoints []message.Outpoint

	iter := db.db.NewIterator(util.BytesPrefix([]byte("outpoint:")), nil)
	defer iter.Release()

	for iter.Next() {
		key := iter.Key()
		// Extract outpoint from key (skip "outpoint:" prefix)
		if len(key) >= 9+message.OutpointSize {
			var outpoint message.Outpoint
			copy(outpoint[:], key[9:9+message.OutpointSize])
			outpoints = append(outpoints, outpoint)
		}
	}

	return outpoints, iter.Error()
}

// ExpireMessages removes messages older than the given duration
func (db *LevelDB) ExpireMessages(maxAge time.Duration) int {
	cutoff := time.Now().Add(-maxAge)
	cutoffUnix := uint64(cutoff.Unix())
	expiredCount := 0

	batch := new(leveldb.Batch)

	// Iterate through outpoints to check timestamps
	iter := db.db.NewIterator(util.BytesPrefix([]byte("outpoint:")), nil)
	defer iter.Release()

	for iter.Next() {
		value := iter.Value()
		if len(value) >= 8 {
			timestamp := binary.LittleEndian.Uint64(value)
			if timestamp < cutoffUnix {
				// Extract outpoint from key
				key := iter.Key()
				if len(key) >= 9+message.OutpointSize {
					var outpoint message.Outpoint
					copy(outpoint[:], key[9:9+message.OutpointSize])

					// Delete both outpoint and message
					batch.Delete(key)
					batch.Delete(makeMessageKey(outpoint))
					expiredCount++
				}
			}
		}
	}

	if expiredCount > 0 {
		err := db.db.Write(batch, nil)
		if err != nil {
			log.Printf("Failed to expire messages: %v", err)
			return 0
		}
	}

	return expiredCount
}

// Helper functions for key generation
func makeOutpointKey(outpoint message.Outpoint) []byte {
	key := make([]byte, 9+message.OutpointSize)
	copy(key, "outpoint:")
	copy(key[9:], outpoint[:])
	return key
}

func makeMessageKey(outpoint message.Outpoint) []byte {
	key := make([]byte, 8+message.OutpointSize)
	copy(key, "message:")
	copy(key[8:], outpoint[:])
	return key
}
