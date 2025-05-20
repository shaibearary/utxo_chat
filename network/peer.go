// Copyright (c) 2023 UTXOchat developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package network

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"

	"github.com/btcsuite/btcd/chaincfg/chainhash"
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
			if err := p.handleInvMessage(reader); err != nil {
				log.Printf("Error handling inv message from peer %s: %v", p.addr, err)
				return
			}

		case MessageTypeGetData:
			// Pass the reader to the handler function
			if err := p.handleGetDataMessage(reader); err != nil {
				log.Printf("Error handling getdata message from peer %s: %v", p.addr, err)
				return
			}

		case MessageTypeData:
			// Pass the reader to the handler function
			if err := p.handleDataMessage(reader); err != nil {
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
func (p *Peer) handleInvMessage(reader *bufio.Reader) error {
	// Read count of inventory items
	countBytes := make([]byte, 2)
	if _, err := io.ReadFull(reader, countBytes); err != nil {
		return fmt.Errorf("failed to read inv count: %v", err)
	}

	count := binary.LittleEndian.Uint16(countBytes)

	// Read each inventory item (txid + vout)
	for i := uint16(0); i < count; i++ {
		outpointBytes := make([]byte, message.OutpointSize)
		if _, err := io.ReadFull(reader, outpointBytes); err != nil {
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
func (p *Peer) handleGetDataMessage(reader *bufio.Reader) error {
	// Read outpoint
	outpointBytes := make([]byte, message.OutpointSize)
	if _, err := io.ReadFull(reader, outpointBytes); err != nil {
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
func (p *Peer) handleDataMessage(reader *bufio.Reader) error {
	// Read the outpoint (36 bytes)
	outpointBuf := make([]byte, message.OutpointSize)
	if _, err := io.ReadFull(reader, outpointBuf); err != nil {
		return fmt.Errorf("failed to read outpoint: %v", err)
	}

	// Read the signature (64 bytes)
	signatureBuf := make([]byte, message.SignatureSize)
	if _, err := io.ReadFull(reader, signatureBuf); err != nil {
		return fmt.Errorf("failed to read signature: %v", err)
	}

	// Read the length (2 bytes)
	lengthBuf := make([]byte, message.LengthSize)
	if _, err := io.ReadFull(reader, lengthBuf); err != nil {
		return fmt.Errorf("failed to read length: %v", err)
	}

	// Extract payload length
	payloadLength := binary.LittleEndian.Uint16(lengthBuf)

	// Check for reasonable size
	if payloadLength > message.MaxPayloadSize {
		return fmt.Errorf("invalid payload length: %d", payloadLength)
	}

	// Allocate buffer for the entire message
	totalSize := message.HeaderSize + int(payloadLength)
	msgData := make([]byte, totalSize)

	// Copy header components to the buffer
	copy(msgData[0:message.OutpointSize], outpointBuf)
	copy(msgData[message.OutpointSize:message.OutpointSize+message.SignatureSize], signatureBuf)
	copy(msgData[message.OutpointSize+message.SignatureSize:message.HeaderSize], lengthBuf)
	// Read the payload if there is any
	// Read the payload directly into the message buffer based on payload length
	payloadBuf := make([]byte, payloadLength)
	if payloadLength > 0 {
		if _, err := io.ReadFull(reader, payloadBuf); err != nil {
			return fmt.Errorf("failed to read message payload: %v", err)
		}
		// Copy payload into the message data buffer
		copy(msgData[message.HeaderSize:], payloadBuf)
	}

	// Log the message parts for debugging
	var outpoint message.Outpoint
	copy(outpoint[:], outpointBuf)
	log.Printf("Received message - Outpoint: %x:%d, Payload length: %d bytes",
		outpointBuf[:32], binary.LittleEndian.Uint32(outpointBuf[32:36]), payloadLength)

	// Deserialize the message
	msg, err := message.Deserialize(msgData)
	if err != nil {
		return fmt.Errorf("failed to deserialize message: %v", err)
	}

	// Validate the message using our validator
	// Get public key from payload (this would depend on your message format)
	pubKeyHex, err := p.extractPubKey(outpoint[:])
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
func (p *Peer) extractPubKey(outpoint []byte) (string, error) {
	// Extract the txid and vout from the outpoint
	txid, _ := message.Outpoint(outpoint).ToTxidIdx()

	// Get the UTXO from Bitcoin node
	// Convert txid to chainhash.Hash (reversing the bytes)
	var hash chainhash.Hash
	for i := 0; i < 32; i++ {
		hash[i] = txid[31-i]
	}

	// Convert vout bytes to uint32 (little-endian)
	voutValue := binary.LittleEndian.Uint32(outpoint[32:36])

	log.Printf("Extracting public key for txid: %s, vout: %d", hash.String(), voutValue)

	txOut, err := p.manager.validator.GetTxOut(&hash, voutValue, false)
	if err != nil {
		return "", fmt.Errorf("failed to get UTXO info: %v", err)
	}

	// Check if the UTXO exists
	if txOut == nil {
		return "", fmt.Errorf("outpoint does not exist or is spent")
	}

	// Check if the UTXO is a taproot output
	if !p.manager.validator.IsTaprootOutput(txOut) {
		return "", fmt.Errorf("outpoint is not a taproot output")
	}

	// Extract the taproot pubkey from the UTXO
	pubKeyHex, err := p.manager.validator.GetTaprootPubKey(txOut)
	if err != nil {
		return "", fmt.Errorf("failed to extract taproot pubkey: %v", err)
	}

	return pubKeyHex, nil
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
