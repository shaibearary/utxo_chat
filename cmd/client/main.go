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
	"strconv"
	"strings"

	"github.com/btcsuite/btcd/btcec/v2/schnorr"

	"github.com/btcsuite/btcd/btcutil/hdkeychain"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
	bip322 "github.com/unisat-wallet/libbrc20-indexer/utils/bip322"
)

// Outpoint represents a Bitcoin transaction output
type Outpoint struct {
	TxID  [32]byte
	Index uint32
}

const (
	// MessageTypeData is sent to deliver messages (from network/peer.go)
	messageTypeData byte = 0x03
	// OutpointSize is the expected byte length of an outpoint (txid + vout index)
	outpointSize = 36
	// SignatureSize is the expected byte length of a signature
	signatureSize = 64
)

func GetSha256(data []byte) (hash []byte) {
	sha := sha256.New()
	sha.Write(data[:])
	hash = sha.Sum(nil)
	return
}
func GetTagSha256(data []byte) (hash []byte) {
	tag := []byte("BIP0322-signed-message")
	hashTag := GetSha256(tag)
	var msg []byte
	msg = append(msg, hashTag...)
	msg = append(msg, hashTag...)
	msg = append(msg, data...)
	return GetSha256(msg)
}

// parseTaprootDescriptor extracts the extended private key and the full
// derivation path from a key-path-only tr(...) descriptor. A wildcard element
// ("*" or "*h") is resolved to wildcardIndex, which is why the caller must
// supply one: the descriptor alone does not name a single key.
func parseTaprootDescriptor(descriptor string, wildcardIndex uint32) (string, []uint32, error) {
	body := strings.TrimSpace(descriptor)
	if !strings.HasPrefix(body, "tr(") {
		return "", nil, fmt.Errorf("descriptor must be a tr(...) descriptor")
	}
	body = strings.TrimPrefix(body, "tr(")

	// Drop the closing parenthesis and any trailing "#checksum".
	end := strings.LastIndex(body, ")")
	if end < 0 {
		return "", nil, fmt.Errorf("descriptor is missing its closing parenthesis")
	}
	body = body[:end]

	if strings.Contains(body, ",") {
		return "", nil, fmt.Errorf("script-path descriptors are not supported; use a key-path-only tr(KEY) descriptor")
	}

	// Strip an optional key origin block such as "[fingerprint/86h/1h/0h]".
	if strings.HasPrefix(body, "[") {
		originEnd := strings.Index(body, "]")
		if originEnd < 0 {
			return "", nil, fmt.Errorf("descriptor has an unterminated key origin block")
		}
		body = body[originEnd+1:]
	}

	parts := strings.Split(body, "/")
	if parts[0] == "" {
		return "", nil, fmt.Errorf("descriptor is missing its extended key")
	}

	// Every element after the key is part of the path. Dropping the last one
	// derives the parent of the intended key and produces a signature that
	// cannot verify against the UTXO's script.
	path := make([]uint32, 0, len(parts)-1)
	for _, part := range parts[1:] {
		index, err := parsePathElement(part, wildcardIndex)
		if err != nil {
			return "", nil, fmt.Errorf("invalid path element %q: %v", part, err)
		}
		path = append(path, index)
	}

	return parts[0], path, nil
}

// parsePathElement converts one BIP-32 path element into a child index.
func parsePathElement(part string, wildcardIndex uint32) (uint32, error) {
	hardened := strings.HasSuffix(part, "h") || strings.HasSuffix(part, "'")
	digits := strings.TrimRight(part, "h'")

	var index uint32
	if digits == "*" {
		index = wildcardIndex
	} else {
		value, err := strconv.ParseUint(digits, 10, 32)
		if err != nil {
			return 0, fmt.Errorf("not a number or wildcard")
		}
		index = uint32(value)
	}

	if index >= hdkeychain.HardenedKeyStart {
		return 0, fmt.Errorf("index %d is out of range", index)
	}
	if hardened {
		index += hdkeychain.HardenedKeyStart
	}

	return index, nil
}

// SignMessageWithTaproot signs a message using BIP322
func SignMessageWithTaproot(descriptor string, wildcardIndex uint32, outpoint Outpoint, message string) ([]byte, error) {
	tprv, path, err := parseTaprootDescriptor(descriptor, wildcardIndex)
	if err != nil {
		return nil, err
	}

	// Parse the extended private key
	extKey, err := hdkeychain.NewKeyFromString(tprv)
	if err != nil {
		return nil, fmt.Errorf("failed to parse extended key: %v", err)
	}

	// Verify it's a private key
	if !extKey.IsPrivate() {
		return nil, fmt.Errorf("descriptor does not contain a private key")
	}

	// Derive through the complete path
	key := extKey
	for _, index := range path {
		key, err = key.Derive(index)
		if err != nil {
			return nil, fmt.Errorf("derivation error at index %d: %v", index, err)
		}
	}

	// Get the private key
	privKey, err := key.ECPrivKey()
	if err != nil {
		return nil, fmt.Errorf("failed to get private key: %v", err)
	}

	// Get the public key
	pubKey, err := key.ECPubKey()
	if err != nil {
		return nil, fmt.Errorf("derivation error: %v", err)
	}

	schnorrPubKey, err := schnorr.ParsePubKey(schnorr.SerializePubKey(pubKey))
	if err != nil {

		return nil, fmt.Errorf("Error converting to Schnorr pubkey: %v\n", err)
	}
	// Create Taproot output key
	taprootKey := txscript.ComputeTaprootOutputKey(schnorrPubKey, nil)
	taprootScript, err := txscript.PayToTaprootScript(taprootKey)
	if err != nil {

		return nil, fmt.Errorf("Error creating Taproot script: %v\n", err)
	}

	// The node verifies against the scriptPubKey of the referenced UTXO, so a
	// mismatch here is the difference between a signature that verifies and one
	// that silently does not.
	log.Printf("Signing with Taproot scriptPubKey %x", taprootScript)

	// Step 1: Create the "to_spend" transaction (virtual tx1)
	toSpend := wire.NewMsgTx(0)
	messageHash := GetTagSha256([]byte(message))
	builder := txscript.NewScriptBuilder()
	builder.AddOp(txscript.OP_0)
	builder.AddData(messageHash)
	scriptSig, err := builder.Script()
	if err != nil {
		return nil, err
	}

	prevOutHash, _ := chainhash.NewHashFromStr("0000000000000000000000000000000000000000000000000000000000000000")

	prevOut := wire.NewOutPoint(prevOutHash, wire.MaxPrevOutIndex)
	txIn := wire.NewTxIn(prevOut, scriptSig, nil)
	txIn.Sequence = 0

	toSpend.AddTxIn(txIn)
	toSpend.AddTxOut(wire.NewTxOut(0, taprootScript))

	toSign := wire.NewMsgTx(0)
	hash := toSpend.TxHash()

	prevOutSpend := wire.NewOutPoint((*chainhash.Hash)(hash.CloneBytes()), 0)

	txSignIn := wire.NewTxIn(prevOutSpend, nil, nil)
	txSignIn.Sequence = 0
	toSign.AddTxIn(txSignIn)

	builderPk := txscript.NewScriptBuilder()
	builderPk.AddOp(txscript.OP_RETURN)
	scriptPk, err := builderPk.Script()
	if err != nil {
		return nil, err
	}
	toSign.AddTxOut(wire.NewTxOut(0, scriptPk))

	// Step 3: Sign the transaction
	prevFetcher := txscript.NewCannedPrevOutputFetcher(taprootScript, 0)
	sigHashes := txscript.NewTxSigHashes(toSign, prevFetcher)

	witness, err := txscript.TaprootWitnessSignature(
		toSign, sigHashes, 0, 0, taprootScript,
		txscript.SigHashDefault, privKey,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create witness signature: %v", err)
	}

	// Verify the signature immediately
	toSign.TxIn[0].Witness = witness
	vm, err := txscript.NewEngine(
		taprootScript,
		toSign,
		0,
		txscript.StandardVerifyFlags,
		nil,
		sigHashes,
		0,
		prevFetcher,
	)
	if err != nil {
		log.Printf("Script engine creation error: %v", err)
		return nil, fmt.Errorf("failed to create script engine: %v", err)
	}
	if err := vm.Execute(); err != nil {
		log.Printf("Script execution error: %v", err)
		log.Printf("Transaction details:")
		log.Printf("  toSign: %+v", toSign)
		log.Printf("  witness: %x", witness)
		log.Printf("  pkScript: %x", taprootScript)
		log.Printf("  messageHash: %x", messageHash)
		return nil, fmt.Errorf("signature verification failed: %v", err)
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

	verifyResult := bip322.VerifySignature(witness, taprootScript, message)
	if !verifyResult {
		return nil, fmt.Errorf("locally generated BIP-322 signature did not verify")
	}
	return msg, nil
}

func main() {
	// Command line flags
	descriptor := flag.String("descriptor", "", "Taproot private descriptor (test keys only)")
	index := flag.Uint("index", 0, "Address index substituted for a wildcard (*) in the descriptor path")
	txid := flag.String("txid", "", "Transaction ID")
	vout := flag.Uint("vout", 0, "Output index")
	message := flag.String("message", "", "Message to sign")
	server := flag.String("server", "127.0.0.1:8335", "UTXO Chat node TCP address")
	flag.Parse()
	if *index > math.MaxUint32 {
		log.Fatal("-index is out of range")
	}
	if *descriptor == "" || *txid == "" || *message == "" {
		log.Fatal("-descriptor, -txid, and -message are required; use a disposable testnet descriptor only")
	}

	var outpoint Outpoint
	txidBytes, err := hex.DecodeString(*txid)
	if err != nil || len(txidBytes) != 32 {
		log.Fatal("-txid must be exactly 64 hexadecimal characters")
	}
	copy(outpoint.TxID[:], txidBytes)
	outpoint.Index = uint32(*vout)

	// Sign message
	msg, err := SignMessageWithTaproot(*descriptor, uint32(*index), outpoint, *message)
	if err != nil {
		log.Fatalf("Error signing message: %v", err)
	}

	// Connect to the UTXO Chat server
	conn, err := net.Dial("tcp", *server)
	if err != nil {
		log.Fatalf("Failed to connect to server: %v", err)
	}
	defer conn.Close()

	// Prepare message with type header (messageTypeData = 0x03)
	fullMsg := make([]byte, 0, 1+len(msg))
	fullMsg = append(fullMsg, messageTypeData)
	fullMsg = append(fullMsg, msg...)

	// Send the message
	_, err = conn.Write(fullMsg)
	if err != nil {
		log.Fatalf("Failed to send message: %v", err)
	}

	fmt.Printf("Successfully sent %d bytes.\n", len(fullMsg))
	fmt.Printf("Submitted message to %s for outpoint %s:%d. The current P2P protocol has no acknowledgement.\n", *server, *txid, *vout)
}
