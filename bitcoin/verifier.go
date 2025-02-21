package bitcoin

import (
	"fmt"
	"strings"

	"encoding/hex"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/ecdsa"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/rpcclient"
)

type UtxoVerifier struct {
	client *rpcclient.Client
}

func NewUtxoVerifier(host string, user string, pass string) (*UtxoVerifier, error) {
	connCfg := &rpcclient.ConnConfig{
		Host:         host,
		User:         user,
		Pass:         pass,
		HTTPPostMode: true,
		DisableTLS:   true,
	}

	client, err := rpcclient.New(connCfg, nil)
	if err != nil {
		return nil, err
	}

	return &UtxoVerifier{
		client: client,
	}, nil
}

func (v *UtxoVerifier) VerifyUtxo(txid string, vout uint32, pubKeyHex string) (bool, error) {
	hash, err := chainhash.NewHashFromStr(txid)
	if err != nil {
		return false, fmt.Errorf("invalid txid: %v", err)
	}

	// Get the UTXO from Bitcoin node
	txOut, err := v.client.GetTxOut(hash, vout, true)
	if err != nil {
		return false, fmt.Errorf("failed to get txout: %v", err)
	}

	// Check if UTXO exists
	if txOut == nil {
		return false, fmt.Errorf("utxo not found")
	}
    // most simple version, p2spk
	// Get the scriptPubKey (output script) from the UTXO
	scriptPubKey := txOut.ScriptPubKey.Hex

	// Check if the provided public key matches the script
	// For P2PKH, the script should contain the hash of the public key
	pubKeyBytes, err := hex.DecodeString(pubKeyHex)
	if err != nil {
		return false, fmt.Errorf("invalid public key hex: %v", err)
	}

	// Hash the public key (RIPEMD160(SHA256(pubkey)))
	pubKeyHash := btcutil.Hash160(pubKeyBytes)

	// The script should contain this hash
	pubKeyHashHex := hex.EncodeToString(pubKeyHash)
	if !strings.Contains(scriptPubKey, pubKeyHashHex) {
		return false, fmt.Errorf("public key does not match utxo owner")
	}

	return true, nil
}

func (v *UtxoVerifier) VerifySignature(message, signature, pubKeyHex []byte) (bool, error) {
	pubKey, err := btcec.ParsePubKey(pubKeyHex)
	if err != nil {
		return false, err
	}

	sig, err := ecdsa.ParseSignature(signature)
	if err != nil {
		return false, err
	}

	// Hash the message first
	messageHash := chainhash.DoubleHashB(message)

	return sig.Verify(messageHash, pubKey), nil
}

func (v *UtxoVerifier) Close() {
	v.client.Shutdown()
}
