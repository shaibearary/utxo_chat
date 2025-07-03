package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
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
	)
	flag.Parse()

	if *command == "" {
		fmt.Println("Usage:")
		fmt.Println("  utxochat-cli -command=sendmessage -message=<hex_data>")
		fmt.Println("  utxochat-cli -command=getmessages")
		os.Exit(1)
	}

	switch *command {
	case "sendmessage":
		if *messageHex == "" {
			fmt.Println("Error: -message flag is required for sendmessage command")
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
		fmt.Printf("Failed to send message: %s\n", response.Error)
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
