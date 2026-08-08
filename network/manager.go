// Copyright (c) 2023 UTXOchat developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package network

import (
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"math/rand"
	"net"
	"sync"
	"time"

	"github.com/shaibearary/utxo_chat/database"
	"github.com/shaibearary/utxo_chat/message"
)

// MessageSource indicates where a message originated from
type MessageSource int

const (
	// MessageSourcePeer indicates the message came from a network peer
	MessageSourcePeer MessageSource = iota
	// MessageSourceLocal indicates the message came from local wallet/RPC
	MessageSourceLocal
)

// Manager handles the network operations for UTXOchat.
type Manager struct {
	config    Config
	validator *database.Validator
	db        database.Database

	peers   map[string]*Peer
	peersMu sync.RWMutex

	listener net.Listener
	quit     chan struct{}
	wg       sync.WaitGroup
	ctx      context.Context
	cancel   context.CancelFunc
}

// NewManager creates a new network manager.
func NewManager(cfg Config, v *database.Validator, db database.Database) (*Manager, error) {
	return &Manager{
		config:    cfg,
		validator: v,
		db:        db,
		peers:     make(map[string]*Peer),
		quit:      make(chan struct{}),
	}, nil
}

// Start initializes the network and starts listening for connections.
func (m *Manager) Start(ctx context.Context) error {
	log.Printf("Starting network manager on %s", m.config.ListenAddr)

	// Store context for background operations
	m.ctx, m.cancel = context.WithCancel(ctx)

	// Start listening for incoming connections
	listener, err := net.Listen("tcp", m.config.ListenAddr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %v", m.config.ListenAddr, err)
	}
	m.listener = listener

	// Accept incoming connections
	m.wg.Add(1)
	go m.acceptConnections(ctx)

	// Start periodic INV broadcasting
	m.wg.Add(1)
	go m.periodicINVBroadcast()

	// Start message expiration cleanup
	m.wg.Add(1)
	go m.messageCleanup()

	// Connect to known peers
	for _, addr := range m.config.KnownPeers {
		if err := m.connectToPeer(addr); err != nil {
			log.Printf("Failed to connect to peer %s: %v", addr, err)
		}
	}

	return nil
}

// Stop shuts down the network manager.
func (m *Manager) Stop() error {
	log.Println("Stopping network manager")

	// Signal all goroutines to quit
	close(m.quit)

	// Cancel context for background operations
	if m.cancel != nil {
		m.cancel()
	}

	// Close listener
	if m.listener != nil {
		m.listener.Close()
	}

	// Snapshot the peer list, then disconnect outside the lock. Disconnect
	// removes each peer from the map, so holding peersMu across the loop would
	// deadlock the shutdown.
	m.peersMu.Lock()
	peers := make([]*Peer, 0, len(m.peers))
	for _, peer := range m.peers {
		peers = append(peers, peer)
	}
	m.peersMu.Unlock()

	for _, peer := range peers {
		peer.Disconnect()
	}

	// Wait for all goroutines to finish
	m.wg.Wait()

	return nil
}

// acceptConnections handles incoming connections.
func (m *Manager) acceptConnections(ctx context.Context) {
	defer m.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case <-m.quit:
			return
		default:
		}

		conn, err := m.listener.Accept()
		if err != nil {
			select {
			case <-m.quit:
				return
			default:
				log.Printf("Error accepting connection: %v", err)
				continue
			}
		}

		// Handle the new connection
		m.wg.Add(1)
		go m.handleConnection(conn)
	}
}

// handleConnection processes a new connection.
func (m *Manager) handleConnection(conn net.Conn) {
	defer m.wg.Done()
	defer conn.Close()

	addr := conn.RemoteAddr().String()
	log.Printf("New connection from %s", addr)

	// Create a new peer
	peer := NewPeer(conn, m)

	// Add peer to the map
	m.peersMu.Lock()
	m.peers[addr] = peer
	m.peersMu.Unlock()

	// Remove peer when done
	defer func() {
		m.peersMu.Lock()
		delete(m.peers, addr)
		m.peersMu.Unlock()
		log.Printf("Connection from %s closed", addr)
	}()

	// Handle peer communication
	peer.Handle()
}

// connectToPeer establishes a connection to a peer.
func (m *Manager) connectToPeer(addr string) error {
	log.Printf("Connecting to peer %s", addr)

	// Check if already connected
	m.peersMu.RLock()
	_, exists := m.peers[addr]
	m.peersMu.RUnlock()
	if exists {
		return fmt.Errorf("already connected to %s", addr)
	}

	// Connect to peer
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to connect to %s: %v", addr, err)
	}

	// Handle the connection
	m.wg.Add(1)
	go m.handleConnection(conn)

	return nil
}

// addMessageToMempool adds a message to the mempool and broadcasts it to peers
func (m *Manager) addMessageToMempool(outpoint message.Outpoint, msgData []byte) error {
	log.Printf("Adding message for outpoint %s to mempool", outpoint.ToString())

	// Store the message in the database
	err := m.db.AddMessage(context.Background(), outpoint, msgData)
	if err != nil {
		return fmt.Errorf("failed to store message in database: %v", err)
	}

	// Broadcast INV to all peers
	return m.broadcastToAllPeers(outpoint)
}

// broadcastToAllPeers broadcasts an outpoint to all connected peers
func (m *Manager) broadcastToAllPeers(outpoint message.Outpoint) error {
	m.peersMu.RLock()
	defer m.peersMu.RUnlock()

	var errors []error
	for _, peer := range m.peers {
		if err := peer.sendInv(outpoint); err != nil {
			log.Printf("Failed to broadcast INV to peer %s: %v", peer.addr, err)
			errors = append(errors, err)
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("failed to broadcast to %d peers", len(errors))
	}
	return nil
}

// getMessageFromDB retrieves a message from the database
func (m *Manager) getMessageFromDB(ctx context.Context, outpoint message.Outpoint) ([]byte, error) {
	return m.db.GetMessage(ctx, outpoint)
}

// periodicINVBroadcast periodically broadcasts INV messages to peers
func (m *Manager) periodicINVBroadcast() {
	defer m.wg.Done()

	ticker := time.NewTicker(30 * time.Second) // Broadcast every 30 seconds
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-m.quit:
			return
		case <-ticker.C:
			// Get all known outpoints
			outpoints, err := m.db.GetAllOutpoints(m.ctx)
			if err != nil {
				log.Printf("Failed to get outpoints for INV broadcast: %v", err)
				continue
			}

			if len(outpoints) == 0 {
				continue
			}

			log.Printf("Broadcasting INV for %d outpoints", len(outpoints))

			// Broadcast a random subset of outpoints to each peer
			m.peersMu.RLock()
			for _, peer := range m.peers {
				// Send a random subset (max 10) to avoid spam
				subset := m.getRandomSubset(outpoints, 10)
				for _, outpoint := range subset {
					if err := peer.sendInv(outpoint); err != nil {
						log.Printf("Failed to send INV to peer %s: %v", peer.addr, err)
					}
				}
			}
			m.peersMu.RUnlock()
		}
	}
}

// messageCleanup periodically removes expired messages
func (m *Manager) messageCleanup() {
	defer m.wg.Done()

	ticker := time.NewTicker(10 * time.Minute) // Clean up every 10 minutes
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-m.quit:
			return
		case <-ticker.C:
			// Remove messages older than 24 hours
			expired := m.db.ExpireMessages(24 * time.Hour)
			if expired > 0 {
				log.Printf("Expired %d old messages", expired)
			}
		}
	}
}

// getRandomSubset returns a random subset of outpoints
func (m *Manager) getRandomSubset(outpoints []message.Outpoint, maxCount int) []message.Outpoint {
	if len(outpoints) <= maxCount {
		return outpoints
	}

	// Simple random selection
	result := make([]message.Outpoint, maxCount)
	perm := rand.Perm(len(outpoints))
	for i := 0; i < maxCount; i++ {
		result[i] = outpoints[perm[i]]
	}
	return result
}

// storeMessageInDB stores a message in the database.
// Note: In a production system, you would enhance database.Database interface to include this
func (m *Manager) storeMessageInDB(ctx context.Context, outpoint message.Outpoint, msgData []byte) error {
	// This is a placeholder implementation
	// In a real implementation, you would call m.db.AddMessage(ctx, outpoint, msgData)
	log.Printf("Storing message for outpoint %s (%d bytes)", outpoint.ToString(), len(msgData))

	// TODO: Implement proper message storage
	return nil
}

// broadcastToOtherPeers sends a message to all connected peers except the source peer.
func (m *Manager) broadcastToOtherPeers(sourcePeer *Peer, outpoint message.Outpoint, msgData []byte) {
	m.peersMu.RLock()
	defer m.peersMu.RUnlock()

	for _, peer := range m.peers {
		// Skip source peer
		if peer == sourcePeer {
			continue
		}

		// Send inventory message
		go func(p *Peer) {
			// Create inv message with this outpoint
			header := make([]byte, 3) // 1 byte type + 2 bytes count (1)
			header[0] = byte(MessageTypeInv)
			binary.LittleEndian.PutUint16(header[1:], 1) // 1 inventory item

			// Add outpoint
			payload := outpoint[:]
			// Combine header and payload
			data := append(header, payload...)

			// Send to peer
			if err := p.SendMessage(MessageTypeInv, data); err != nil {
				log.Printf("Failed to broadcast to peer %s: %v", p.addr, err)
			}
		}(peer)
	}
}

// removePeerFromList removes a peer from the peer list.
func (m *Manager) removePeerFromList(peer *Peer) {
	addr := peer.addr

	m.peersMu.Lock()
	defer m.peersMu.Unlock()

	if _, exists := m.peers[addr]; exists {
		delete(m.peers, addr)
		log.Printf("Removed peer %s from list", addr)
	}
}

// ProcessMessage handles a message based on its source
func (m *Manager) ProcessMessage(ctx context.Context, msgData []byte, source MessageSource) error {
	// Deserialize the message
	msg, err := message.Deserialize(msgData)
	if err != nil {
		return fmt.Errorf("failed to deserialize message: %v", err)
	}

	switch source {
	case MessageSourcePeer:
		return m.processPeerMessage(ctx, msg, msgData)
	case MessageSourceLocal:
		return m.processLocalMessage(ctx, msg, msgData)
	default:
		return fmt.Errorf("unknown message source: %d", source)
	}
}

// processPeerMessage handles messages received from network peers (full validation)
func (m *Manager) processPeerMessage(ctx context.Context, msg *message.Message, msgData []byte) error {
	log.Printf("Processing peer message for outpoint %s", msg.Outpoint.ToString())

	// Check if we've already seen this outpoint (rate limiting)
	seen, err := m.db.HasOutpoint(ctx, msg.Outpoint)
	if err != nil {
		return fmt.Errorf("database error: %v", err)
	}
	if seen {
		log.Printf("Outpoint %s already seen, ignoring", msg.Outpoint.ToString())
		return nil // Not an error, just ignore duplicate
	}

	// Extract public key script for validation
	pkScript, err := m.extractPKScript(msg.Outpoint)
	if err != nil {
		return fmt.Errorf("failed to extract public key script: %v", err)
	}

	// Full validation for peer messages
	if err := m.validator.ValidateMessage(ctx, msg, pkScript); err != nil {
		return fmt.Errorf("peer message validation failed: %v", err)
	}

	// Store the message
	if err := m.db.AddMessage(ctx, msg.Outpoint, msgData); err != nil {
		return fmt.Errorf("failed to store peer message: %v", err)
	}

	// Broadcast to other peers (but not back to sender)
	return m.broadcastToAllPeers(msg.Outpoint)
}

// processLocalMessage handles messages from local wallet/RPC (minimal validation)
func (m *Manager) processLocalMessage(ctx context.Context, msg *message.Message, msgData []byte) error {
	log.Printf("Processing local wallet message for outpoint %s", msg.Outpoint.ToString())

	// For local messages, we trust they're already validated by the wallet
	// Just check for duplicates
	seen, err := m.db.HasOutpoint(ctx, msg.Outpoint)
	if err != nil {
		return fmt.Errorf("database error: %v", err)
	}
	if seen {
		return fmt.Errorf("outpoint %s already used", msg.Outpoint.ToString())
	}

	// Store the message without full validation
	if err := m.db.AddMessage(ctx, msg.Outpoint, msgData); err != nil {
		return fmt.Errorf("failed to store local message: %v", err)
	}

	// Mark outpoint as used
	if err := m.db.AddOutpoint(ctx, msg.Outpoint); err != nil {
		return fmt.Errorf("failed to mark outpoint as used: %v", err)
	}

	// Broadcast to all peers
	return m.broadcastToAllPeers(msg.Outpoint)
}

// extractPKScript extracts the public key script for UTXO validation
func (m *Manager) extractPKScript(outpoint message.Outpoint) ([]byte, error) {
	hash, vout := outpoint.ToTxidIdx()

	// Get UTXO from Bitcoin node
	txOut, err := m.validator.GetTxOut(hash, vout, false)
	if err != nil {
		return nil, fmt.Errorf("failed to get UTXO: %v", err)
	}

	if txOut == nil {
		return nil, fmt.Errorf("UTXO not found or already spent")
	}

	// Get the script from the UTXO
	if m.validator.IsTaprootOutput(txOut) {
		return m.validator.GetTaprootPKScript(txOut)
	}

	// For other output types, you'd implement similar extraction logic
	return nil, fmt.Errorf("unsupported UTXO type")
}

// GetAllOutpoints returns all known outpoints from the database
func (m *Manager) GetAllOutpoints(ctx context.Context) ([]message.Outpoint, error) {
	return m.db.GetAllOutpoints(ctx)
}
