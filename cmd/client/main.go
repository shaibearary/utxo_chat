// UTXO Chat - A decentralized messaging system using Bitcoin UTXOs
// Copyright (C) 2024 UTXO Chat developers
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package main

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"math"
	"net"
	"strings"

	"github.com/btcsuite/btcd/btcutil/hdkeychain"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
)

// Outpoint represents a Bitcoin transaction output
type Outpoint struct {
	TxID  [32]byte
	Index uint32
}

const (
	// MessageTypeData is sent to deliver messages (from network/peer.go)
	messageTypeData byte = 0x03
	// ServerAddress is the address the UTXO Chat node listens on
	serverAddress = "localhost:8335"
	// OutpointSize is the expected byte length of an outpoint (txid + vout index)
	outpointSize = 36
	// SignatureSize is the expected byte length of a signature
	signatureSize = 64
)

// SignMessageWithTaproot signs a message using BIP322
func SignMessageWithTaproot(descriptor string, outpoint Outpoint, message string) ([]byte, error) {
	// Parse descriptor
	desc := strings.TrimPrefix(descriptor, "tr(")
	desc = strings.Split(desc, ")#")[0]
	parts := strings.Split(desc, "/")

	// Get base key
	tprv := parts[0]

	// Parse the extended private key
	extKey, err := hdkeychain.NewKeyFromString(tprv)
	if err != nil {
		return nil, fmt.Errorf("failed to parse tprv: %v", err)
	}

	// Verify it's a private key
	if !extKey.IsPrivate() {
		return nil, fmt.Errorf("not a private key")
	}

	// Derive through path
	key := extKey
	for _, part := range parts[1 : len(parts)-1] {
		var index uint32
		if strings.HasSuffix(part, "h") {
			num := strings.TrimSuffix(part, "h")
			fmt.Sscanf(num, "%d", &index)
			index += hdkeychain.HardenedKeyStart
		} else {
			fmt.Sscanf(part, "%d", &index)
		}

		key, err = key.Derive(index)
		if err != nil {
			return nil, fmt.Errorf("derivation error: %v", err)
		}
	}

	// Get the private key
	privKey, err := key.ECPrivKey()
	if err != nil {
		return nil, fmt.Errorf("failed to get private key: %v", err)
	}

	// Get the public key
	pubKey := privKey.PubKey()

	// Create the taproot script
	pkScript, err := txscript.PayToTaprootScript(pubKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create taproot script: %v", err)
	}

	// Step 1: Create the "to_spend" transaction (virtual tx1)
	toSpend := wire.NewMsgTx(0)
	prevOut := wire.NewOutPoint(&chainhash.Hash{}, math.MaxUint32)
	txIn := wire.NewTxIn(prevOut, nil, nil)
	txIn.Sequence = 0
	toSpend.AddTxIn(txIn)

	// Add scriptSig with message hash
	tag := []byte("BIP0322-signed-message")
	messageBytes := []byte(message)
	h := sha256.New()
	h.Write(tag)
	h.Write(tag)
	h.Write(messageBytes)
	messageHash := h.Sum(nil)

	builder := txscript.NewScriptBuilder()
	builder.AddOp(txscript.OP_0)
	builder.AddData(messageHash[:])
	scriptSig, _ := builder.Script()
	toSpend.TxIn[0].SignatureScript = scriptSig

	// Add output with the taproot script
	toSpend.AddTxOut(wire.NewTxOut(0, pkScript))

	// Step 2: Create the "to_sign" transaction (virtual tx2)
	toSign := wire.NewMsgTx(0)
	spendHash := toSpend.TxHash()
	txIn = wire.NewTxIn(wire.NewOutPoint(&spendHash, 0), nil, nil)
	txIn.Sequence = 0
	toSign.AddTxIn(txIn)

	// Add OP_RETURN output
	builder = txscript.NewScriptBuilder()
	builder.AddOp(txscript.OP_RETURN)
	opReturnScript, _ := builder.Script()
	toSign.AddTxOut(wire.NewTxOut(0, opReturnScript))

	// Step 3: Sign the transaction
	prevFetcher := txscript.NewCannedPrevOutputFetcher(pkScript, 0)
	sigHashes := txscript.NewTxSigHashes(toSign, prevFetcher)

	witness, err := txscript.TaprootWitnessSignature(
		toSign, sigHashes, 0, 0, pkScript,
		txscript.SigHashDefault, privKey,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create witness signature: %v", err)
	}

	// Create the final message structure
	msg := make([]byte, 0, 102+len(message))

	// Add outpoint (36 bytes)
	msg = append(msg, outpoint.TxID[:]...)
	indexBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(indexBytes, outpoint.Index)
	msg = append(msg, indexBytes...)

	// Add signature (64 bytes)
	if len(witness) > 0 {
		msg = append(msg, witness[0]...)
	}

	// Add length (2 bytes)
	length := uint16(len(message))
	lengthBytes := make([]byte, 2)
	binary.LittleEndian.PutUint16(lengthBytes, length)
	msg = append(msg, lengthBytes...)

	// Add payload
	msg = append(msg, []byte(message)...)

	// Log the different parts of the message structure
	log.Printf("Message structure breakdown:")
	log.Printf("  Outpoint (%d bytes): %x", len(outpoint.TxID)+4, msg[:outpointSize])
	log.Printf("  Signature (%d bytes): %x", signatureSize, msg[outpointSize:outpointSize+signatureSize])
	log.Printf("  Length field (%d bytes): %x (decimal: %d)", 2, msg[outpointSize+signatureSize:outpointSize+signatureSize+2], length)
	log.Printf("  Payload (%d bytes): %s", len(message), message)
	log.Printf("Total message size: %d bytes", len(msg))

	return msg, nil
}

func main() {
	// Command line flags
	descriptor := flag.String("descriptor", "tr(tprv8ZgxMBicQKsPeWrZe5tMwsV3m7CtZdHRH9sas8S6D87NwVrMgUh9NdoyC9mZYJSNojGWiDSw9NAZspQFzVp9i6KRoKQxQvMspYEp64JW6nF/86h/1h/0h/0/*)#7j579lp7", "Taproot descriptor")
	txid := flag.String("txid", "77c6c3037ac3dd953aff22f9dff8ebca4f947e29b0bb6d348c1aaa049a79d24a", "Transaction ID")
	vout := flag.Uint("vout", 0, "Output index")
	message := flag.String("message", "Hello, UTXO Chat!", "Message to sign")
	flag.Parse()

	var outpoint Outpoint
	txidBytes, _ := hex.DecodeString(*txid)
	copy(outpoint.TxID[:], txidBytes)
	outpoint.Index = uint32(*vout)

	// Sign message
	msg, err := SignMessageWithTaproot(*descriptor, outpoint, *message)
	if err != nil {
		log.Fatalf("Error signing message: %v", err)
	}
	// Connect to the UTXO Chat server
	conn, err := net.Dial("tcp", serverAddress)
	if err != nil {
		log.Fatalf("Failed to connect to server: %v", err)
	}
	defer conn.Close()

	// Prepare message with type header (messageTypeData = 0x03)
	fullMsg := []byte{messageTypeData}
	fullMsg = append(fullMsg, msg...)

	// Send the message
	_, err = conn.Write(fullMsg)
	if err != nil {
		log.Fatalf("Failed to send message: %v", err)
	}

	log.Printf("Message sent successfully to %s", serverAddress)

}
