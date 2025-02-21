package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
)

type ChatMessage struct {
	Content   []byte `json:"content"`
	Signature []byte `json:"signature"`
	PublicKey string `json:"public_key"`
	UtxoTxid  string `json:"utxo_txid"`
	UtxoVout  uint32 `json:"utxo_vout"`
}

func main() {
	content := flag.String("message", "Hello UTXO Chat!", "Message content to send")
	flag.Parse()

	msg := ChatMessage{
		Content:   []byte(*content),
		Signature: []byte("304402deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"),
		PublicKey: "02deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
		UtxoTxid:  "1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
		UtxoVout:  0,
	}

	fmt.Printf("%+v\n", msg)

	jsonData, err := json.Marshal(msg)
	if err != nil {
		fmt.Printf("Error marshaling JSON: %v\n", err)
		return
	}

	resp, err := http.Post("http://127.0.0.1:8333/message", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		fmt.Printf("Error sending message: %v\n", err)
		return
	}
	defer resp.Body.Close()

	fmt.Printf("Response Status: %s\n", resp.Status)
}
