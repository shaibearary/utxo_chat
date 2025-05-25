package message

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/btcsuite/btcd/chaincfg/chainhash"
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
type Outpoint [36]byte

func (op Outpoint) ToTxidIdx() (*chainhash.Hash, uint32) {
	// ignoring the returned error here since we are giving it 32 bytes from a
	// fixed 36 byte array, and the only possible error is due to incorrect
	// array length
	// Create a reversed copy of the txid bytes for chainhash.NewHash
	// since Bitcoin displays txids in big-endian but internally uses little-endian
	reversedTxid := make([]byte, 32)
	for i := 0; i < 32; i++ {
		reversedTxid[i] = op[31-i]
	}
	hash, _ := chainhash.NewHash(reversedTxid)
	return hash, binary.LittleEndian.Uint32(op[32:36])
}

func (op Outpoint) ToString() string {
	return fmt.Sprintf("%x:%d", op[:32], binary.BigEndian.Uint32(op[32:36]))
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
	copy(buf[0:36], m.Outpoint[:])

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
	copy(msg.Outpoint[:], data[0:36])

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
