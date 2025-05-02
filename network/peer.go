// Copyright (c) 2023 UTXOchat developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package network

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"

	"github.com/shaibearary/utxo_chat/message"
)

// MessageType defines the type of message being sent
type MessageType byte

const (
	// MessageTypeInv is sent to announce known messages
	MessageTypeInv MessageType = 0x01
	// MessageTypeGetData is sent to request messages
	MessageTypeGetData MessageType = 0x02
	// MessageTypeData is sent to deliver messages
	MessageTypeData MessageType = 0x03
)

// Peer represents a connected peer
type Peer struct {
	conn       net.Conn
	manager    *Manager
	addr       string
	connected  bool
	disconnect chan struct{}
	mutex      sync.Mutex // Protects fields from concurrent access
	ctx        context.Context
}

// NewPeer creates a new peer
func NewPeer(conn net.Conn, manager *Manager) *Peer {
	return &Peer{
		conn:       conn,
		manager:    manager,
		addr:       conn.RemoteAddr().String(),
		connected:  true,
		disconnect: make(chan struct{}),
		ctx:        context.Background(),
	}
}

// Handle starts handling communication with the peer
func (p *Peer) Handle() {
	// Set read deadline for the initial handshake
	p.conn.SetReadDeadline(time.Now().Add(60 * time.Second))

	// TODO: Implement peer handshake

	// If we get here, handshake was successful
	// Reset the deadline for normal operation
	p.conn.SetReadDeadline(time.Time{})

	// Start reading messages from peer
	p.readMessages()

}

// readMessages reads and processes incoming messages from the peer
func (p *Peer) readMessages() {
	defer func() {
		p.Disconnect()
	}()
	reader := bufio.NewReader(p.conn)

	for {

		select {
		case <-p.disconnect:
			log.Printf("Disconnect signal received for peer %s", p.addr)
			return
		default:
		}

		// Log the incoming message
		log.Printf("Receiving message from peer %s", p.addr)

		// --- Read Message Type ---
		// Read exactly one byte for the message type
		msgTypeByte, err := reader.ReadByte()
		if err != nil {
			// Handle common errors cleanly
			if err == io.EOF {
				log.Printf("Connection closed by peer %s (EOF)", p.addr)
			} else if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				log.Printf("Read timeout from peer %s: %v", p.addr, err)
				// You might want to continue here or disconnect depending on your protocol
			} else if opErr, ok := err.(*net.OpError); ok && opErr.Err.Error() == "use of closed network connection" {
				// This specific check might be redundant if EOF covers it, but can be explicit
				log.Printf("Attempted read on closed connection from peer %s", p.addr)
			} else {
				log.Printf("Error reading message type from peer %s: %v", p.addr, err)
			}
			return // Disconnect on any read error
		}

		msgType := MessageType(msgTypeByte)
		log.Printf("Received message type %d (0x%x) from peer %s", msgType, msgType, p.addr)

		// --- Process based on message type ---
		// Now read the rest of the message based on its type
		switch msgType {
		case MessageTypeInv:
			// Pass the reader to the handler function
			if err := p.handleInvMessage(); err != nil {
				log.Printf("Error handling inv message from peer %s: %v", p.addr, err)
				return
			}

		case MessageTypeGetData:
			// Pass the reader to the handler function
			if err := p.handleGetDataMessage(); err != nil {
				log.Printf("Error handling getdata message from peer %s: %v", p.addr, err)
				return
			}

		case MessageTypeData:
			// Pass the reader to the handler function
			if err := p.handleDataMessage(); err != nil {
				log.Printf("Error handling data message from peer %s: %v", p.addr, err)
				return
			}

		default:
			log.Printf("Received unknown message type %d from peer %s. Disconnecting.", msgType, p.addr)
			return // Disconnect on unknown type
		}
	}
}

// handleInvMessage processes an inventory message from a peer
func (p *Peer) handleInvMessage() error {
	// Read count of inventory items
	countBytes := make([]byte, 2)
	if _, err := io.ReadFull(p.conn, countBytes); err != nil {
		return fmt.Errorf("failed to read inv count: %v", err)
	}

	count := binary.LittleEndian.Uint16(countBytes)

	// Read each inventory item (txid + vout)
	for i := uint16(0); i < count; i++ {
		outpointBytes := make([]byte, message.OutpointSize)
		if _, err := io.ReadFull(p.conn, outpointBytes); err != nil {
			return fmt.Errorf("failed to read outpoint %d: %v", i, err)
		}
		var outpoint message.Outpoint
		copy(outpoint[:], outpointBytes[:])

		// Check in the database if we've already seen this outpoint
		hasOutpoint, err := p.manager.db.HasOutpoint(p.ctx, outpoint)
		if err != nil {
			log.Printf("Error checking outpoint in database: %v", err)
			continue
		}

		// If we don't have it, request it
		if !hasOutpoint {
			// Queue a get data request
			go p.requestData(outpoint)
		}
	}

	return nil
}

// handleGetDataMessage processes a get data message from a peer
func (p *Peer) handleGetDataMessage() error {
	// Read outpoint
	outpointBytes := make([]byte, message.OutpointSize)
	if _, err := io.ReadFull(p.conn, outpointBytes); err != nil {
		return fmt.Errorf("failed to read outpoint: %v", err)
	}

	// Convert to outpoint
	var outpoint message.Outpoint
	copy(outpoint[:], outpointBytes[:])

	// Get the message from database
	msgData, err := p.manager.getMessageFromDB(p.ctx, outpoint)
	if err != nil {
		return fmt.Errorf("failed to get message from database: %v", err)
	}

	// If we don't have the message, ignore
	if msgData == nil {
		log.Printf("Peer requested message we don't have: %s", outpoint.ToString())
		return nil
	}

	// Send the message
	return p.sendDataMessage(msgData)
}

// handleDataMessage processes a data message from a peer
func (p *Peer) handleDataMessage() error {
	// Read message length
	lengthBytes := make([]byte, 4)
	if _, err := io.ReadFull(p.conn, lengthBytes); err != nil {
		return fmt.Errorf("failed to read message length: %v", err)
	}

	length := binary.LittleEndian.Uint32(lengthBytes)

	// Check for reasonable size
	if length == 0 || length > message.MaxMessageSize {
		return fmt.Errorf("invalid message length: %d", length)
	}

	// Read the message data
	msgData := make([]byte, length)
	if _, err := io.ReadFull(p.conn, msgData); err != nil {
		return fmt.Errorf("failed to read message data: %v", err)
	}

	// Deserialize the message
	msg, err := message.Deserialize(msgData)
	if err != nil {
		return fmt.Errorf("failed to deserialize message: %v", err)
	}

	// Validate the message using our validator
	// Get public key from payload (this would depend on your message format)
	pubKeyHex, err := extractPubKey(msg.Payload)
	if err != nil {
		return fmt.Errorf("failed to extract public key: %v", err)
	}

	// Use context from peer
	if err := p.manager.validator.ValidateMessage(p.ctx, msg, pubKeyHex); err != nil {
		return fmt.Errorf("invalid message: %v", err)
	}

	// If valid, save to database and broadcast to other peers

	// Store original message data in database
	if err := p.manager.storeMessageInDB(p.ctx, msg.Outpoint, msgData); err != nil {
		return fmt.Errorf("failed to save message to database: %v", err)
	}

	// Broadcast to other peers
	p.manager.broadcastToOtherPeers(p, msg.Outpoint, msgData)

	return nil
}

// Helper function to extract public key from payload
// The format will depend on your specific implementation
func extractPubKey(payload []byte) (string, error) {
	// TODO: Implement actual public key extraction based on your message format
	// This is just a placeholder
	if len(payload) < 33 {
		return "", fmt.Errorf("payload too short to contain public key")
	}

	// Assuming the first part of the payload contains the public key
	return hex.EncodeToString(payload[:33]), nil
}

// requestData sends a getdata message to the peer
func (p *Peer) requestData(outpoint message.Outpoint) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if !p.connected {
		return fmt.Errorf("peer disconnected")
	}

	// Prepare getdata message
	msgBytes := make([]byte, 1+message.OutpointSize)
	msgBytes[0] = byte(MessageTypeGetData)
	copy(msgBytes[1:37], outpoint[:])

	// Send message
	_, err := p.conn.Write(msgBytes)
	return err
}

// sendDataMessage sends a data message to the peer
func (p *Peer) sendDataMessage(msgData []byte) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if !p.connected {
		return fmt.Errorf("peer disconnected")
	}

	// Prepare data message header
	header := make([]byte, 5) // 1 byte type + 4 bytes length
	header[0] = byte(MessageTypeData)
	binary.LittleEndian.PutUint32(header[1:], uint32(len(msgData)))

	// Send header
	if _, err := p.conn.Write(header); err != nil {
		return err
	}

	// Send message data
	_, err := p.conn.Write(msgData)
	return err
}

// SendMessage sends a message to the peer
func (p *Peer) SendMessage(msgType MessageType, data []byte) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if !p.connected {
		return fmt.Errorf("peer disconnected")
	}

	// Write message type
	if _, err := p.conn.Write([]byte{byte(msgType)}); err != nil {
		return err
	}

	// Write data
	_, err := p.conn.Write(data)
	return err
}

// Disconnect closes the connection to the peer
func (p *Peer) Disconnect() {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if !p.connected {
		return
	}

	log.Printf("Disconnecting peer %s", p.addr)

	// Close connection
	p.conn.Close()
	p.connected = false

	// Signal disconnect
	close(p.disconnect)

	// Log closure *before* removing from list
	log.Printf("Connection from %s closed", p.addr)

	// Remove from manager's peer list
	p.manager.removePeerFromList(p)
}
