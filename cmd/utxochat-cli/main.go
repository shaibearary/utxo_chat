package main

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/shaibearary/utxo_chat/message"
)

// SendMessageRequest represents a request to send a message
type SendMessageRequest struct {
	MessageData string `json:"message_data"`
}

// SendMessageResponse represents the response to a send message request
type SendMessageResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
	TxID    string `json:"txid,omitempty"`
}

// GetMessagesResponse represents the response to get messages request
type GetMessagesResponse struct {
	Messages []MessageInfo `json:"messages"`
	Count    int           `json:"count"`
}

// MessageInfo represents information about a stored message
type MessageInfo struct {
	Outpoint string `json:"outpoint"`
	Size     int    `json:"size"`
}

func main() {
	var (
		rpcURL     = flag.String("rpcurl", "http://127.0.0.1:8335", "RPC server URL")
		command    = flag.String("command", "", "Command to execute (sendmessage, getmessages)")
		messageHex = flag.String("message", "", "Hex-encoded message data for sendmessage")
		txid       = flag.String("txid", "", "Displayed Bitcoin transaction ID (64 hex characters)")
		vout       = flag.Uint("vout", 0, "UTXO output index")
		signature  = flag.String("signature", "", "Sparrow BIP-322 signature (hex or base64)")
		payload    = flag.String("payload", "", "Exact payload that was signed")
	)
	flag.Parse()

	if *command == "" {
		fmt.Println("Usage:")
		fmt.Println("  utxochat-cli -command=sendmessage -message=<hex_data>")
		fmt.Println("  utxochat-cli -command=send-signed -txid=<txid> -vout=<n> -signature=<hex-or-base64> -payload=<text>")
		fmt.Println("  utxochat-cli -command=getmessages")
		os.Exit(1)
	}

	switch *command {
	case "sendmessage":
		if *messageHex == "" {
			fmt.Println("Error: -message flag is required for sendmessage command")
			os.Exit(1)
		}
	case "send-signed":
		if *txid == "" || *signature == "" {
			fmt.Println("Error: -txid and -signature are required for send-signed")
			os.Exit(1)
		}
		if err := sendSignedMessage(*rpcURL, *txid, uint32(*vout), *signature, *payload); err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}
		err := sendMessage(*rpcURL, *messageHex)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}

	case "getmessages":
		err := getMessages(*rpcURL)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}

	default:
		fmt.Printf("Unknown command: %s\n", *command)
		os.Exit(1)
	}
}

// sendSignedMessage assembles Sparrow's signature with the public outpoint and
// exact payload, then submits the serialized DATA body to the local node. No
// private key or wallet descriptor is accepted or required.
func sendSignedMessage(rpcURL, txid string, vout uint32, signatureText, payload string) error {
	txid = strings.TrimPrefix(strings.TrimSpace(txid), "0x")
	txidBytes, err := hex.DecodeString(txid)
	if err != nil || len(txidBytes) != 32 {
		return fmt.Errorf("txid must be exactly 64 hexadecimal characters")
	}
	sig, err := decodeSignature(signatureText)
	if err != nil {
		return err
	}
	if len([]byte(payload)) > message.MaxPayloadSize {
		return message.ErrMessageTooLarge
	}
	body := make([]byte, message.HeaderSize+len([]byte(payload)))
	copy(body[0:32], txidBytes)
	binary.LittleEndian.PutUint32(body[32:36], vout)
	copy(body[36:100], sig)
	binary.LittleEndian.PutUint16(body[100:102], uint16(len([]byte(payload))))
	copy(body[102:], []byte(payload))
	return sendMessage(rpcURL, hex.EncodeToString(body))
}

func decodeSignature(text string) ([]byte, error) {
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "0x")
	if sig, err := hex.DecodeString(text); err == nil && len(sig) == message.SignatureSize {
		return sig, nil
	}
	if sig, err := base64.StdEncoding.DecodeString(text); err == nil && len(sig) == message.SignatureSize {
		return sig, nil
	}
	return nil, fmt.Errorf("signature must decode to exactly 64 bytes of hex or base64")
}

func sendMessage(rpcURL, messageHex string) error {
	fmt.Printf("Sending message to %s\n", messageHex)
	req := SendMessageRequest{
		MessageData: messageHex,
	}

	jsonData, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %v", err)
	}

	resp, err := http.Post(rpcURL+"/sendmessage", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to send request: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %v", err)
	}

	var response SendMessageResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return fmt.Errorf("failed to parse response: %v", err)
	}

	if response.Success {
		fmt.Printf("Message sent successfully!\n")
		if response.TxID != "" {
			fmt.Printf("Transaction ID: %s\n", response.TxID)
		}
	} else {
		return fmt.Errorf("node rejected message: %s", response.Error)
	}

	return nil
}

func getMessages(rpcURL string) error {
	resp, err := http.Get(rpcURL + "/getmessages")
	if err != nil {
		return fmt.Errorf("failed to send request: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %v", err)
	}

	var response GetMessagesResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return fmt.Errorf("failed to parse response: %v", err)
	}

	fmt.Printf("Found %d messages:\n", response.Count)
	for i, msg := range response.Messages {
		fmt.Printf("%d. Outpoint: %s\n", i+1, msg.Outpoint)
	}

	return nil
}
