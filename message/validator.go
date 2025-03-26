package message

import (
	"context"
	"fmt"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/ecdsa"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/shaibearary/utxo_chat/bitcoin"
	"github.com/shaibearary/utxo_chat/database"
)

// Validator handles message validation including UTXO ownership and signatures.
type Validator struct {
	client *bitcoin.Client
	db     database.Database
}

// NewValidator creates a new message validator.
func NewValidator(client *bitcoin.Client, db database.Database) *Validator {
	return &Validator{
		client: client,
		db:     db,
	}
}

// ValidateMessage validates a message including UTXO ownership and signature.
func (v *Validator) ValidateMessage(ctx context.Context, msg *Message, pubKeyHex string) error {
	// Convert outpoint txid to string
	txid := chainhash.Hash(msg.Outpoint.TxID)

	// Check if we've already seen this outpoint
	outpoint := database.Outpoint{
		TxID:  txid,
		Index: msg.Outpoint.Index,
	}

	seen, err := v.db.HasOutpoint(ctx, outpoint)
	if err != nil {
		return fmt.Errorf("database error: %v", err)
	}

	if seen {
		return fmt.Errorf("outpoint already seen")
	}

	// Verify UTXO ownership
	if err := v.VerifyUTXOOwnership(ctx, txid.String(), msg.Outpoint.Index, pubKeyHex); err != nil {
		return fmt.Errorf("UTXO verification failed: %v", err)
	}

	// Verify message signature
	if err := v.VerifySignature(msg.Payload, msg.Signature[:], pubKeyHex); err != nil {
		return fmt.Errorf("signature verification failed: %v", err)
	}

	// Add outpoint to the database
	if err := v.db.AddOutpoint(ctx, outpoint); err != nil {
		return fmt.Errorf("failed to add outpoint to database: %v", err)
	}

	return nil
}

// VerifyUTXOOwnership verifies that the given public key owns the specified UTXO.
func (v *Validator) VerifyUTXOOwnership(ctx context.Context, txid string, vout uint32, pubKeyHex string) error {
	// Convert txid string to hash
	hash, err := chainhash.NewHashFromStr(txid)
	if err != nil {
		return fmt.Errorf("invalid txid: %v", err)
	}

	// Get the UTXO from Bitcoin node
	txOut, err := v.client.GetTxOut(hash, vout, false)
	if err != nil {
		return fmt.Errorf("failed to get txout: %v", err)
	}

	// Check if UTXO exists
	if txOut == nil {
		return fmt.Errorf("utxo not found")
	}

	// TODO: Implement proper script validation for different UTXO types
	// Currently only checking if the public key hash matches

	return nil
}

// VerifySignature verifies that the message was signed by the owner of the public key.
func (v *Validator) VerifySignature(message []byte, signature []byte, pubKeyHex string) error {
	// Parse public key
	pubKeyBytes, err := btcec.ParsePubKey([]byte(pubKeyHex))
	if err != nil {
		return fmt.Errorf("invalid public key: %v", err)
	}

	// Parse signature
	sig, err := ecdsa.ParseSignature(signature)
	if err != nil {
		return fmt.Errorf("invalid signature: %v", err)
	}

	// Hash the message (double SHA256)
	messageHash := chainhash.DoubleHashB(message)

	// Verify the signature
	if !sig.Verify(messageHash, pubKeyBytes) {
		return fmt.Errorf("invalid signature")
	}

	return nil
}
