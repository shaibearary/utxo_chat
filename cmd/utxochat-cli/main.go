package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"

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
		// Go does not fall through, so this call has to live here. While it
		// sat in the send-signed case, sendmessage validated its input and
		// then exited without submitting anything, and send-signed followed a
		// successful submission with a second, empty one.
		if err := sendMessage(*rpcURL, *messageHex); err != nil {
			fmt.Printf("Error: %v\n", err)
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
// private key or wallet descriptor is accepted or required. The assembly is
// shared with the node's /submitsigned endpoint so both paths produce
// byte-identical messages.
func sendSignedMessage(rpcURL, txid string, vout uint32, signatureText, payload string) error {
	// Validate the inputs locally so an obvious typo is reported here rather
	// than as an opaque rejection from the node.
	if _, err := message.BuildSigned(txid, vout, signatureText, []byte(payload)); err != nil {
		return err
	}

	req := SubmitSignedRequest{
		TxID:      txid,
		Vout:      vout,
		Signature: signatureText,
		Payload:   payload,
	}
	jsonData, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %v", err)
	}

	// /submitsigned rather than /sendmessage: the node verifies the signature
	// against the UTXO there. /sendmessage stores whatever it is given, so a
	// mistyped signature would be accepted locally and then rejected by every
	// peer, with the outpoint already spent as far as this node is concerned.
	resp, err := http.Post(rpcURL+"/submitsigned", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to send request: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %v", err)
	}

	var response SubmitSignedResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return fmt.Errorf("failed to parse response: %v", err)
	}
	if !response.Success {
		return fmt.Errorf("node rejected message: %s", response.Error)
	}

	fmt.Printf("Message sent successfully!\n")
	fmt.Printf("Outpoint: %s\n", response.Outpoint)
	return nil
}

// SubmitSignedRequest mirrors the node's /submitsigned payload.
type SubmitSignedRequest struct {
	TxID      string `json:"txid"`
	Vout      uint32 `json:"vout"`
	Signature string `json:"signature"`
	Payload   string `json:"payload"`
}

// SubmitSignedResponse is the node's reply to a /submitsigned request.
type SubmitSignedResponse struct {
	Success  bool   `json:"success"`
	Error    string `json:"error,omitempty"`
	Outpoint string `json:"outpoint,omitempty"`
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
