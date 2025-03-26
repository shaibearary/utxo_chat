package main

import (
	"bytes"
	"encoding/hex"
	"flag"
	"fmt"
	"net/http"

	"github.com/shaibearary/utxo_chat/message"
)

func main() {
	content := flag.String("message", "Hello UTXO Chat!", "Message content to send")
	txid := flag.String("txid", "", "UTXO transaction ID")
	vout := flag.Uint("vout", 0, "UTXO output index")
	signature := flag.String("signature", "", "Message signature (64 bytes hex)")
	flag.Parse()

	// Convert txid to bytes
	txidBytes, err := hex.DecodeString(*txid)
	if err != nil {
		fmt.Printf("Error decoding txid: %v\n", err)
		return
	}
	var txidArray [32]byte
	copy(txidArray[:], txidBytes)

	// Convert signature to bytes
	sigBytes, err := hex.DecodeString(*signature)
	if err != nil {
		fmt.Printf("Error decoding signature: %v\n", err)
		return
	}
	var sigArray [64]byte
	copy(sigArray[:], sigBytes)

	// Create outpoint
	outpoint := message.Outpoint{
		TxID:  txidArray,
		Index: uint32(*vout),
	}

	// Create message
	msg, err := message.NewMessage(outpoint, sigArray, []byte(*content))
	if err != nil {
		fmt.Printf("Error creating message: %v\n", err)
		return
	}

	// Serialize message
	data := msg.Serialize()

	// Send message to server
	resp, err := http.Post("http://127.0.0.1:8335/message", "application/octet-stream", bytes.NewBuffer(data))
	if err != nil {
		fmt.Printf("Error sending message: %v\n", err)
		return
	}
	defer resp.Body.Close()

	fmt.Printf("Response Status: %s\n", resp.Status)
}
