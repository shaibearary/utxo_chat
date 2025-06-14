// Copyright (c) 2023 UTXOchat developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package blockchain

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/shaibearary/utxo_chat/bitcoin"
	"github.com/shaibearary/utxo_chat/database"
	"github.com/shaibearary/utxo_chat/message"
)

// Handler is responsible for monitoring the blockchain and handling new blocks
type Handler struct {
	client *bitcoin.Client
	db     database.Database
	config Config
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
}

// NewHandler creates a new block handler.
func NewHandler(client *bitcoin.Client, db database.Database) *Handler {
	return NewHandlerWithConfig(client, db, DefaultConfig())
}

// NewHandlerWithConfig creates a new block handler with the specified configuration.
func NewHandlerWithConfig(client *bitcoin.Client, db database.Database, config Config) *Handler {
	return &Handler{
		client: client,
		db:     db,
		config: config,
		done:   make(chan struct{}),
	}
}

// Start begins the block notification and processing.
func (h *Handler) Start(ctx context.Context) error {
	h.ctx, h.cancel = context.WithCancel(ctx)

	log.Println("Starting blockchain handler")

	// Get initial blockchain info to determine starting point
	info, err := h.client.GetBlockchainInfo(h.ctx)
	if err != nil {
		return fmt.Errorf("failed to get initial blockchain info: %v", err)
	}

	log.Printf("Initial blockchain state: chain=%s, height=%d", info.Chain, info.Blocks)

	// TODO: Subscribe to block notifications from the Bitcoin client if enabled
	if h.config.NotificationsEnabled {
		// This would typically involve:
		// 1. Setting up a notification handler
		// 2. Registering for block notifications
		log.Println("Block notifications are enabled but not implemented yet, falling back to polling")
	}

	// Start processing in background
	go h.processBlocks()

	return nil
}

// Stop shuts down the block handler.
func (h *Handler) Stop() error {
	log.Println("Stopping blockchain handler")

	// TODO: Unsubscribe from block notifications if enabled
	if h.config.NotificationsEnabled {
		// Unregister notifications
	}

	if h.cancel != nil {
		h.cancel()
	}

	// Wait for processing to complete with timeout
	select {
	case <-h.done:
		log.Println("Blockchain handler stopped gracefully")
	case <-time.After(5 * time.Second):
		log.Println("Blockchain handler stop timed out")
	}

	return nil
}

// processBlocks handles incoming block notifications
func (h *Handler) processBlocks() {
	defer close(h.done)

	log.Printf("Block handler processing started with options: notifications=%v, maxReorgDepth=%d, fullScan=%v",
		h.config.NotificationsEnabled, h.config.MaxReorgDepth, h.config.ScanFullBlocks)

	// Set up polling interval if notifications are not enabled
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	lastKnownHeight := int32(0)

	for {
		select {
		case <-h.ctx.Done():
			return

		case <-ticker.C:
			if !h.config.NotificationsEnabled {
				// If notifications are disabled, poll for new blocks
				info, err := h.client.GetBlockchainInfo(h.ctx)
				if err != nil {
					log.Printf("Error getting blockchain info: %v", err)
					continue
				}

				if info.Blocks > lastKnownHeight {
					log.Printf("New block(s) detected. Previous height: %d, Current height: %d",
						lastKnownHeight, info.Blocks)

					// Process blocks from lastKnownHeight+1 to current height
					for height := lastKnownHeight + 1; height <= info.Blocks; height++ {
						if err := h.handleNewBlock(height); err != nil {
							log.Printf("Error processing block at height %d: %v", height, err)
						}
					}

					lastKnownHeight = info.Blocks
				}
			}

			// TODO: Add a case for block notifications if enabled
			// case block := <-blockNotificationChannel:
			//     h.handleNewBlock(block)
		}
	}
}

// handleNewBlock processes a new block
func (h *Handler) handleNewBlock(height int32) error {

	// Get the block hash for this height
	blockHash, err := h.client.GetBlockHash(h.ctx, height)
	if err != nil {
		return fmt.Errorf("failed to get block hash for height %d: %v", height, err)
	}

	// Get the block data
	block, err := h.client.GetBlock(h.ctx, blockHash)
	if err != nil {
		return fmt.Errorf("failed to get block %s: %v", blockHash.String(), err)
	}

	// Extract all spent outpoints from the block
	spentOutpoints, err := h.extractSpentOutpoints(block)
	if err != nil {
		return fmt.Errorf("failed to extract spent outpoints from block %s: %v", blockHash.String(), err)
	}

	if len(spentOutpoints) > 0 {
		log.Printf("Found %d spent outpoints in block %s", len(spentOutpoints), blockHash.String())

		// Remove spent outpoints from the database
		if err := h.db.RemoveOutpoints(h.ctx, spentOutpoints); err != nil {
			return fmt.Errorf("failed to remove spent outpoints from database: %v", err)
		}

		log.Printf("Removed %d spent outpoints from UTXOchat database", len(spentOutpoints))
	}

	return nil
}

// extractSpentOutpoints extracts all outpoints that are spent in the given block
func (h *Handler) extractSpentOutpoints(block *btcjson.GetBlockVerboseResult) ([]message.Outpoint, error) {
	var spentOutpoints []message.Outpoint

	// Get the block with transaction details
	blockHash, err := chainhash.NewHashFromStr(block.Hash)
	if err != nil {
		return nil, fmt.Errorf("invalid block hash: %v", err)
	}

	// Get verbose block data with transaction details (verbosity level 2)
	blockVerbose, err := h.client.GetBlockVerboseTx(blockHash)
	if err != nil {
		log.Printf("Failed to get block verbose data, falling back to individual tx calls: %v", err)
		return h.extractSpentOutpointsFromTxIDs(block)
	}

	// Process each transaction in the verbose block
	for _, tx := range blockVerbose.Tx {
		// Process each input in the transaction
		for _, input := range tx.Vin {
			// Skip coinbase transactions (they don't spend existing UTXOs)
			if input.Coinbase != "" {
				continue
			}

			// Convert the spent outpoint to our format
			spentOutpoint, err := h.convertToOutpoint(input.Txid, input.Vout)
			if err != nil {
				log.Printf("Failed to convert outpoint %s:%d: %v", input.Txid, input.Vout, err)
				continue
			}

			spentOutpoints = append(spentOutpoints, spentOutpoint)
		}
	}

	return spentOutpoints, nil
}

// extractSpentOutpointsFromTxIDs is a fallback method using individual transaction calls
func (h *Handler) extractSpentOutpointsFromTxIDs(block *btcjson.GetBlockVerboseResult) ([]message.Outpoint, error) {
	var spentOutpoints []message.Outpoint

	log.Printf("Using fallback method for block %s (requires txindex=1)", block.Hash)

	// Process each transaction in the block
	for _, txid := range block.Tx {
		// Parse the transaction ID
		txHash, err := chainhash.NewHashFromStr(txid)
		if err != nil {
			log.Printf("Invalid transaction ID %s: %v", txid, err)
			continue
		}

		// Get the raw transaction to access its inputs
		tx, err := h.client.GetRawTransaction(h.ctx, txHash)
		if err != nil {
			log.Printf("Failed to get raw transaction %s: %v (hint: enable txindex=1 in bitcoin.conf)", txid, err)
			continue
		}

		// Process each input in the transaction
		for _, input := range tx.Vin {
			// Skip coinbase transactions (they don't spend existing UTXOs)
			if input.Coinbase != "" {
				continue
			}

			// Convert the spent outpoint to our format
			spentOutpoint, err := h.convertToOutpoint(input.Txid, input.Vout)
			if err != nil {
				log.Printf("Failed to convert outpoint %s:%d: %v", input.Txid, input.Vout, err)
				continue
			}

			spentOutpoints = append(spentOutpoints, spentOutpoint)
		}
	}

	return spentOutpoints, nil
}

// convertToOutpoint converts a txid string and vout to our Outpoint format
func (h *Handler) convertToOutpoint(txidStr string, vout uint32) (message.Outpoint, error) {
	var outpoint message.Outpoint

	// Parse the transaction hash
	txHash, err := chainhash.NewHashFromStr(txidStr)
	if err != nil {
		return outpoint, fmt.Errorf("invalid txid: %v", err)
	}

	// Copy the transaction hash (in little-endian format for our Outpoint)
	// The chainhash.Hash is already in little-endian format internally
	copy(outpoint[:32], txHash[:])

	// Set the vout (output index) in little-endian format
	outpoint[32] = byte(vout)
	outpoint[33] = byte(vout >> 8)
	outpoint[34] = byte(vout >> 16)
	outpoint[35] = byte(vout >> 24)

	return outpoint, nil
}
