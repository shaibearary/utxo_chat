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
	"io"
	"log"
	"net"
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
	// ServerAddress is the address the UTXO Chat node listens on
	serverAddress = "localhost:8335"
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

// SignMessageWithTaproot signs a message using BIP322
func SignMessageWithTaproot(descriptor string, outpoint Outpoint, message string) ([]byte, error) {
	// Parse descriptor
	desc := strings.TrimPrefix(descriptor, "tr(")
	desc = strings.Split(desc, ")#")[0]
	parts := strings.Split(desc, "/")

	// Get base key

	tprv := parts[0]
	log.Printf("Descriptor parts: %v", parts)
	log.Printf("Full descriptor: %s", desc)

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
	log.Printf("Derivation path parts: %v", parts)
	log.Printf("Number of path parts: %d", len(parts))
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
		log.Printf("Derived key at path %s: %s", part, key.String())
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
	log.Printf("Derived public key: %x", pubKey.SerializeCompressed())

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
	// Create the taproot script

	log.Printf("Generated pkScript: %x", taprootScript)
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

	// Log the different parts of the message structure
	log.Printf("Message structure breakdown:")
	log.Printf("  Outpoint (%d bytes): %x", len(outpoint.TxID)+4, msg[:outpointSize])
	log.Printf("  Signature (%d bytes): %x", signatureSize, msg[outpointSize:outpointSize+signatureSize])
	log.Printf("  Length field (%d bytes): %x (decimal: %d)", 2, msg[outpointSize+signatureSize:outpointSize+signatureSize+2], length)
	log.Printf("  Payload (%d bytes): %s", len(message), message)
	log.Printf("Total message size: %d bytes", len(msg))
	log.Printf("Witness: %x", witness)
	log.Printf("PkScript: %x", taprootScript)
	log.Printf("Message: %s", message)
	verifyResult := bip322.VerifySignature(witness, taprootScript, message)
	log.Printf("Signature verification result: %v", verifyResult)
	return msg, nil
}

func main() {
	// Command line flags
	descriptor := flag.String("descriptor", "tr(tprv8ZgxMBicQKsPd9tkUFdaFQ3HSViR6rSQD75YToUJusnMd64hw2rwecHJohLZswiYa3mXEErjfkk79fo8jRbVeYzuHtTRB214iZz3s9kJYxM/86h/1h/0h/0/0/)#svs6tee0", "Taproot descriptor")
	txid := flag.String("txid", "f63e8bae313e2f88a086b6927a81fe25ec43da550db8d714575abd1c22422021", "Transaction ID")
	vout := flag.Uint("vout", 1, "Output index")
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
	fullMsg := make([]byte, 0, 1+len(msg))
	fullMsg = append(fullMsg, messageTypeData)
	fullMsg = append(fullMsg, msg...)

	// Send the message
	_, err = conn.Write(fullMsg)
	if err != nil {
		log.Fatalf("Failed to send message: %v", err)
	}

	fmt.Printf("Successfully sent %d bytes.\n", len(fullMsg))
	// Print the full message in hex for debugging
	fmt.Println("Full message hex dump:")
	fmt.Printf("%x\n", fullMsg)

	// Print a more detailed breakdown of the message
	fmt.Println("\nMessage breakdown:")
	fmt.Printf("Message Type: %x\n", fullMsg[0])
	fmt.Printf("Outpoint (txid+vout): %x\n", fullMsg[1:37])
	fmt.Printf("Signature: %x\n", fullMsg[37:101])
	fmt.Printf("Length field: %x (decimal: %d)\n", fullMsg[101:103], binary.LittleEndian.Uint16(fullMsg[101:103]))
	fmt.Printf("Payload: %s\n", fullMsg[103:])

	// Wait for server response

	fmt.Println("Waiting for server response...")
	response := make([]byte, 1024)
	n, err := conn.Read(response)
	if err != nil {
		if err != io.EOF {
			log.Printf("Error reading response: %v", err)
		}
	} else {
		fmt.Printf("Received response (%d bytes): %s\n", n, response[:n])
	}
}
