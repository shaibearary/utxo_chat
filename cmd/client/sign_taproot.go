package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/btcsuite/btcd/btcutil/hdkeychain"
)

// SignMessageWithTaproot signs a message using a Taproot private key
func waitForGetBlockResSignMessageWithTaproot(tprv string, message string) (string, string, error) {
	// Parse the extended private key
	// Check tprv length
	if len(tprv) != 111 {
		return "", "", fmt.Errorf("invalid tprv length: got %d, want 111", len(tprv))
	}
	extKey, err := hdkeychain.NewKeyFromString(tprv)
	if err != nil {
		return "", "", fmt.Errorf("failed to parse tprv: %v", err)
	}

	// Verify it's a private key
	if !extKey.IsPrivate() {
		return "", "", fmt.Errorf("not a private key")
	}

	// Get the actual private key from the extended key
	privKey, err := extKey.ECPrivKey()
	if err != nil {
		return "", "", fmt.Errorf("failed to get private key: %v", err)
	}

	// Create tagged hash (BIP340 style)
	tag := []byte("BIP0322-signed-message")
	messageBytes := []byte(message)
	h := sha256.New()
	h.Write(tag)
	h.Write(tag)
	h.Write(messageBytes)
	messageHash := h.Sum(nil)

	// Sign the message using Schnorr signature
	signature, err := schnorr.Sign(privKey, messageHash)
	if err != nil {
		return "", "", fmt.Errorf("failed to sign message: %v", err)
	}

	// Get the public key
	pubKey := privKey.PubKey()

	// Convert to hex strings
	sigHex := hex.EncodeToString(signature.Serialize())
	pubKeyHex := hex.EncodeToString(schnorr.SerializePubKey(pubKey))

	return sigHex, pubKeyHex, nil
}
