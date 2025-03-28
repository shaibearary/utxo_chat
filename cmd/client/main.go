package main

import (
	"encoding/hex"
	"flag"
	"fmt"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/shaibearary/utxo_chat/message"
)

func main() {
	content := flag.String("message", "Hello UTXO Chat!", "Message content to send")
	txid := flag.String("txid", "", "UTXO transaction ID")
	vout := flag.Uint("vout", 0, "UTXO output index")
	pkScript := flag.String("pkscript", "", "Script pubkey (hex)")
	privKeyHex := flag.String("privkey", "", "Private key (hex)")
	flag.Parse()

	// Convert txid to bytes
	txidBytes, err := hex.DecodeString(*txid)
	if err != nil {
		fmt.Printf("Error decoding txid: %v\n", err)
		return
	}
	var txidArray [32]byte
	copy(txidArray[:], txidBytes)

	// Convert pkscript to bytes
	pkScriptBytes, err := hex.DecodeString(*pkScript)
	if err != nil {
		fmt.Printf("Error decoding pkscript: %v\n", err)
		return
	}

	// Convert private key
	privKeyBytes, err := hex.DecodeString(*privKeyHex)
	if err != nil {
		fmt.Printf("Error decoding private key: %v\n", err)
		return
	}
	privKey, _ := btcec.PrivKeyFromBytes(privKeyBytes)

	// Create outpoint for signing
	outpoint := Outpoint{
		TxID:     txidArray,
		Vout:     uint32(*vout),
		PKScript: pkScriptBytes,
	}

	// Sign the message
	sigData, err := SignMessage(outpoint, *content, privKey)
	if err != nil {
		fmt.Printf("Error signing message: %v\n", err)
		return
	}

	// Print the results
	fmt.Printf("Message signed successfully!\n")
	fmt.Printf("Message Hash: %x\n", sigData.MessageHash)
	fmt.Printf("PKScript: %x\n", sigData.PKScript)
	fmt.Printf("Witness stack (%d items):\n", len(sigData.Witness))
	for i, item := range sigData.Witness {
		fmt.Printf("  [%d]: %x\n", i, item)
	}

	// Create UTXO chat message
	msgOutpoint := message.Outpoint{
		TxID:  txidArray,
		Index: uint32(*vout),
	}

	// Use the first witness item as the signature
	var sigArray [64]byte
	if len(sigData.Witness) > 0 {
		copy(sigArray[:], sigData.Witness[0])
	}

	// Create and serialize message
	msg, err := message.NewMessage(msgOutpoint, sigArray, []byte(*content))
	if err != nil {
		fmt.Printf("Error creating message: %v\n", err)
		return
	}

	fmt.Printf("\nFinal serialized message: %x\n", msg.Serialize())
}
