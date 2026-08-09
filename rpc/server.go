package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/shaibearary/utxo_chat/network"
)

// Server handles RPC requests for UTXOchat
type Server struct {
	networkManager *network.Manager
	listenAddr     string
}

// NewServer creates a new RPC server
func NewServer(networkManager *network.Manager, listenAddr string) *Server {
	return &Server{
		networkManager: networkManager,
		listenAddr:     listenAddr,
	}
}

// Start starts the RPC server.
//
// The routes go on a private mux rather than http.DefaultServeMux. The
// profiling server in main.go also serves DefaultServeMux, and binds it to
// every interface, so registering here would publish these endpoints far
// beyond the configured listen address whenever profiling is enabled.
func (s *Server) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/sendmessage", s.handleSendMessage)
	mux.HandleFunc("/getmessages", s.handleGetMessages)

	log.Printf("Starting RPC server on %s", s.listenAddr)
	return http.ListenAndServe(s.listenAddr, mux)
}

// SendMessageRequest represents a request to send a message
type SendMessageRequest struct {
	MessageData string `json:"message_data"` // Hex-encoded message data
}

// SendMessageResponse represents the response to a send message request
type SendMessageResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
	TxID    string `json:"txid,omitempty"`
}

// handleSendMessage handles the sendmessage RPC call (similar to bitcoin-cli sendtransaction)
func (s *Server) handleSendMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req SendMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.sendErrorResponse(w, fmt.Sprintf("Invalid request: %v", err))
		return
	}

	// Decode hex message data
	msgData, err := decodeHexString(req.MessageData)
	if err != nil {
		s.sendErrorResponse(w, fmt.Sprintf("Invalid hex data: %v", err))
		return
	}

	// Process as local wallet message (minimal validation)
	err = s.networkManager.ProcessMessage(context.Background(), msgData, network.MessageSourceLocal)
	if err != nil {
		s.sendErrorResponse(w, fmt.Sprintf("Failed to process message: %v", err))
		return
	}

	// Send success response
	response := SendMessageResponse{
		Success: true,
		TxID:    "message_sent", // In a real implementation, you might return a message ID
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
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

// handleGetMessages handles the getmessages RPC call
func (s *Server) handleGetMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get all outpoints from database
	outpoints, err := s.networkManager.GetAllOutpoints(context.Background())
	if err != nil {
		s.sendErrorResponse(w, fmt.Sprintf("Failed to get messages: %v", err))
		return
	}

	// Convert to response format
	messages := make([]MessageInfo, len(outpoints))
	for i, outpoint := range outpoints {
		messages[i] = MessageInfo{
			Outpoint: outpoint.ToString(),
			Size:     0, // You could get actual size from database if needed
		}
	}

	response := GetMessagesResponse{
		Messages: messages,
		Count:    len(messages),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// sendErrorResponse sends an error response
func (s *Server) sendErrorResponse(w http.ResponseWriter, errorMsg string) {
	response := SendMessageResponse{
		Success: false,
		Error:   errorMsg,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	json.NewEncoder(w).Encode(response)
}

// decodeHexString decodes a hex string to bytes
func decodeHexString(hexStr string) ([]byte, error) {
	// Remove 0x prefix if present
	if len(hexStr) >= 2 && hexStr[:2] == "0x" {
		hexStr = hexStr[2:]
	}

	// Decode hex
	data := make([]byte, len(hexStr)/2)
	for i := 0; i < len(data); i++ {
		var b byte
		_, err := fmt.Sscanf(hexStr[i*2:i*2+2], "%02x", &b)
		if err != nil {
			return nil, err
		}
		data[i] = b
	}

	return data, nil
}
