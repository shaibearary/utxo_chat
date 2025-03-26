// Copyright (c) 2023 UTXOchat developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package blockchain

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/shaibearary/utxo_chat/bitcoin"
	"github.com/shaibearary/utxo_chat/database"
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
	ticker := time.NewTicker(30 * time.Second)
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

	return nil
}
