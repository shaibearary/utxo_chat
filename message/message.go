package message

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	// OutpointSize is the size of an outpoint (txid + vout)
	OutpointSize = 36 // 32 bytes for txid + 4 bytes for vout

	// SignatureSize is the size of a signature
	SignatureSize = 64

	// LengthSize is the size of the length field
	LengthSize = 2

	// HeaderSize is the total size of the header (outpoint + signature + length)
	HeaderSize = OutpointSize + SignatureSize + LengthSize

	// MaxPayloadSize is the maximum size of the payload
	// Application define own data structure within the payload
	MaxPayloadSize = 65434

	// MaxMessageSize is the maximum size of a complete message
	MaxMessageSize = HeaderSize + MaxPayloadSize
)

var (
	ErrMessageTooLarge = errors.New("message exceeds maximum size")
	ErrInvalidHeader   = errors.New("invalid message header")
)

// Outpoint represents a Bitcoin transaction output
type Outpoint struct {
	TxID  [32]byte // Transaction ID
	Index uint32   // Output index
}

// Message represents a UTXOchat message
type Message struct {
	Outpoint  Outpoint // The UTXO that proves ownership
	Signature [64]byte // The signature proving ownership of the UTXO
	Length    uint16   // Length of the payload
	Payload   []byte   // The actual message content
}

// NewMessage creates a new message with the given parameters
func NewMessage(outpoint Outpoint, signature [64]byte, payload []byte) (*Message, error) {
	if len(payload) > MaxPayloadSize {
		return nil, ErrMessageTooLarge
	}

	return &Message{
		Outpoint:  outpoint,
		Signature: signature,
		Length:    uint16(len(payload)),
		Payload:   payload,
	}, nil
}

// Serialize converts the message to a byte slice
func (m *Message) Serialize() []byte {
	buf := make([]byte, HeaderSize+len(m.Payload))

	// Write outpoint
	copy(buf[0:32], m.Outpoint.TxID[:])
	binary.LittleEndian.PutUint32(buf[32:36], m.Outpoint.Index)

	// Write signature
	copy(buf[36:100], m.Signature[:])

	// Write payload length
	binary.LittleEndian.PutUint16(buf[100:102], m.Length)

	// Write payload
	copy(buf[102:], m.Payload)

	return buf
}

// Deserialize parses a byte slice into a message
func Deserialize(data []byte) (*Message, error) {
	if len(data) < HeaderSize {
		return nil, ErrInvalidHeader
	}

	msg := &Message{}

	// Read outpoint
	copy(msg.Outpoint.TxID[:], data[0:32])
	msg.Outpoint.Index = binary.LittleEndian.Uint32(data[32:36])

	// Read signature
	copy(msg.Signature[:], data[36:100])

	// Read payload length
	msg.Length = binary.LittleEndian.Uint16(data[100:102])

	// Validate payload length
	if msg.Length > MaxPayloadSize {
		return nil, ErrMessageTooLarge
	}

	// Read payload
	if len(data) < HeaderSize+int(msg.Length) {
		return nil, fmt.Errorf("message data too short: expected %d bytes, got %d", HeaderSize+msg.Length, len(data))
	}
	msg.Payload = make([]byte, msg.Length)
	copy(msg.Payload, data[102:102+msg.Length])

	return msg, nil
}
