// Copyright (c) 2023 UTXOchat developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package network

import (
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"sync"

	"github.com/shaibearary/utxo_chat/database"
	"github.com/shaibearary/utxo_chat/message"
)

// Manager handles the network operations for UTXOchat.
type Manager struct {
	config    Config
	validator *message.Validator
	db        database.Database

	peers   map[string]*Peer
	peersMu sync.RWMutex

	listener net.Listener
	quit     chan struct{}
	wg       sync.WaitGroup
}

// NewManager creates a new network manager.
func NewManager(cfg Config, v *message.Validator, db database.Database) (*Manager, error) {
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

	// Start listening for incoming connections
	listener, err := net.Listen("tcp", m.config.ListenAddr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %v", m.config.ListenAddr, err)
	}
	m.listener = listener

	// Accept incoming connections
	m.wg.Add(1)
	go m.acceptConnections(ctx)

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

	// Close listener
	if m.listener != nil {
		m.listener.Close()
	}

	// Disconnect all peers
	m.peersMu.Lock()
	for _, peer := range m.peers {
		peer.Disconnect()
	}
	m.peersMu.Unlock()

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

// getMessageFromDB retrieves a message from the database by outpoint.
// Note: In a production system, you would enhance database.Database interface to include this
func (m *Manager) getMessageFromDB(ctx context.Context, outpoint database.Outpoint) ([]byte, error) {
	// This is a placeholder implementation
	// In a real implementation, you would call m.db.GetMessage(ctx, outpoint)
	log.Printf("Getting message for outpoint %x:%d", outpoint.TxID[:], outpoint.Index)

	// TODO: Implement proper message storage and retrieval
	// For now, just return nil (message not found)
	return nil, nil
}

// storeMessageInDB stores a message in the database.
// Note: In a production system, you would enhance database.Database interface to include this
func (m *Manager) storeMessageInDB(ctx context.Context, outpoint database.Outpoint, msgData []byte) error {
	// This is a placeholder implementation
	// In a real implementation, you would call m.db.AddMessage(ctx, outpoint, msgData)
	log.Printf("Storing message for outpoint %x:%d (%d bytes)", outpoint.TxID[:], outpoint.Index, len(msgData))

	// TODO: Implement proper message storage
	return nil
}

// broadcastToOtherPeers sends a message to all connected peers except the source peer.
func (m *Manager) broadcastToOtherPeers(sourcePeer *Peer, outpoint database.Outpoint, msgData []byte) {
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
			payload := make([]byte, message.OutpointSize)
			copy(payload[:32], outpoint.TxID[:])
			binary.LittleEndian.PutUint32(payload[32:], outpoint.Index)

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
