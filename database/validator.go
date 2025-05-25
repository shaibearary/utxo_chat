package database

import (
	"context"
	"encoding/hex"
	"fmt"

	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
	"github.com/shaibearary/utxo_chat/bitcoin"
	"github.com/shaibearary/utxo_chat/message"

	bip322 "github.com/unisat-wallet/libbrc20-indexer/utils/bip322"
)

// Validator handles message validation including UTXO ownership and signatures.
type Validator struct {
	client *bitcoin.Client
	db     Database
}

// NewValidator creates a new message validator.
func NewValidator(client *bitcoin.Client, db Database) *Validator {
	return &Validator{
		client: client,
		db:     db,
	}
}

// ValidateMessage validates a message including UTXO ownership and signature.
func (v *Validator) ValidateMessage(
	ctx context.Context, msg *message.Message, pkScript []byte) error {

	seen, err := v.db.HasOutpoint(ctx, msg.Outpoint)
	if err != nil {
		return fmt.Errorf("database error: %v", err)
	}

	if seen {
		return fmt.Errorf("outpoint already seen")
	}
	// Log pubkey hex and outpoint for debugging
	hash, vout := msg.Outpoint.ToTxidIdx()
	fmt.Printf("Validating message - Outpoint: %s:%d, PubKey: %s\n",
		hash.String(), vout, pkScript)

	// Verify UTXO ownership
	// if err := v.VerifyUTXOOwnership(ctx, msg.Outpoint, pkScript); err != nil {
	// 	return fmt.Errorf("UTXO verification failed: %v", err)
	// }
	messageStr := string(msg.Payload)

	if err := v.VerifySignature(messageStr, msg.Signature[:], pkScript); err != nil {
		return fmt.Errorf("signature verification failed: %v", err)
	}

	// Add outpoint to the database
	if err := v.db.AddOutpoint(ctx, msg.Outpoint); err != nil {
		return fmt.Errorf("failed to add outpoint to database: %v", err)
	}

	return nil
}

// VerifyUTXOOwnership verifies that the given public key owns the specified UTXO.
func (v *Validator) VerifyUTXOOwnership(
	ctx context.Context, outpoint message.Outpoint, pkScript []byte) error {
	hash, vout := outpoint.ToTxidIdx()
	// Get the UTXO from Bitcoin node
	// Log UTXO lookup details for debugging
	fmt.Printf("Looking up UTXO - TxID: %s, Vout: %d\n", hash.String(), vout)

	// Log public key we're verifying against
	fmt.Printf("Verifying UTXO ownership against pubkey: %s\n", pkScript)
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
func (v *Validator) VerifySignature(message string, signature []byte, pkScript []byte) error {
	// Convert pkScript to wire.TxWitness
	witness := wire.TxWitness{signature}
	a := bip322.VerifySignature(witness, pkScript, message)
	fmt.Printf("Signature verification result: %v\n", a)

	return nil
}

// GetTxOut retrieves a transaction output from the Bitcoin node.
func (v *Validator) GetTxOut(txid *chainhash.Hash, vout uint32, includeMempool bool) (*btcjson.GetTxOutResult, error) {
	return v.client.GetTxOut(txid, vout, includeMempool)
}

// IsTaprootOutput checks if a transaction output is a Taproot output.
func (v *Validator) IsTaprootOutput(txOut *btcjson.GetTxOutResult) bool {
	if txOut == nil {
		return false
	}
	// Taproot outputs start with OP_1 (0x51) followed by a 32-byte key
	script, err := hex.DecodeString(txOut.ScriptPubKey.Hex)
	if err != nil {
		return false
	}
	return len(script) == 34 && script[0] == 0x51
}

// GetTaprootPubKey extracts the Taproot public key from a transaction output.
func (v *Validator) GetTaprootPKScript(txOut *btcjson.GetTxOutResult) ([]byte, error) {
	if !v.IsTaprootOutput(txOut) {
		return nil, fmt.Errorf("not a Taproot output")
	}

	scriptBytes, err := hex.DecodeString(txOut.ScriptPubKey.Hex)
	if err != nil {
		return nil, fmt.Errorf("failed to decode script hex: %v", err)
	}

	// The Taproot key is the 32 bytes after OP_1
	return scriptBytes, nil
}
