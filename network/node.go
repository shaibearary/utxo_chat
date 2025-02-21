package network

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"utxo-chat/bitcoin"
	"utxo-chat/message"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/ecdsa"
)

type NodeConfig struct {
	ListenAddr string
	PrivateKey string // hex-encoded private key
	UtxoTxid   string // The UTXO this node owns
	UtxoVout   uint32
}

type Node struct {
	config       NodeConfig
	verifier     *bitcoin.UtxoVerifier
	messageStore map[string][]byte
	mutex        sync.RWMutex
	// peerManager  *PeerManager
	privateKey *btcec.PrivateKey
}

func NewNode(config NodeConfig, verifier *bitcoin.UtxoVerifier) (*Node, error) {
	// Decode and validate private key
	privKeyBytes, err := hex.DecodeString(config.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("invalid private key: %v", err)
	}

	privateKey, _ := btcec.PrivKeyFromBytes(privKeyBytes)

	return &Node{
		config:       config,
		verifier:     verifier,
		messageStore: make(map[string][]byte),
		// peerManager:  NewPeerManager(100),
		privateKey: privateKey,
	}, nil
}

// CreateSignedMessage creates and signs a new chat message
func (n *Node) SignMessage(content []byte) (*message.ChatMessage, error) {
	// Create message
	msg := &message.ChatMessage{
		Content:   content,
		PublicKey: hex.EncodeToString(n.privateKey.PubKey().SerializeCompressed()),
		UtxoTxid:  n.config.UtxoTxid,
		UtxoVout:  n.config.UtxoVout,
	}

	// Sign the message
	signature := ecdsa.Sign(n.privateKey, content)
	msg.Signature = signature.Serialize()

	// Verify our own message first
	valid, err := n.verifier.VerifyUtxo(msg.UtxoTxid, msg.UtxoVout, msg.PublicKey)
	if err != nil || !valid {
		return nil, fmt.Errorf("failed to verify own UTXO: %v", err)
	}

	return msg, nil
}

// BroadcastMessage stores and broadcasts a message to peers
func (n *Node) BroadcastMessage(msg *message.ChatMessage) error {
	msgBytes, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %v", err)
	}

	n.mutex.Lock()
	n.messageStore[msg.UtxoTxid] = msgBytes
	n.mutex.Unlock()

	// TODO: broadcast message to other peers

	return nil
}

// CreateAndBroadcastMessage combines message creation and broadcasting (for backward compatibility)
func (n *Node) CreateAndBroadcastMessage(content []byte) error {
	msg, err := n.SignMessage(content)
	if err != nil {
		return err
	}

	return n.BroadcastMessage(msg)
}

func (h *Node) StartNode(addr string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/message", h.handleHTTPMessage)
	return http.ListenAndServe(addr, mux)
}

func (h *Node) handleHTTPMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var msg message.ChatMessage
	if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	fmt.Printf("Received message with public key: %s\n", msg.PublicKey)

	// Verify UTXO ownership
	valid, err := h.verifier.VerifyUtxo(msg.UtxoTxid, msg.UtxoVout, msg.PublicKey)
	if err != nil {
		fmt.Printf("UTXO verification error: %v\n", err)
		http.Error(w, fmt.Sprintf("UTXO verification failed: %v", err), http.StatusBadRequest)
		return
	}
	if !valid {
		fmt.Printf("UTXO verification failed: invalid ownership\n")
		http.Error(w, "UTXO verification failed: invalid ownership", http.StatusBadRequest)
		return
	}

	// Verify signature
	valid, err = h.verifier.VerifySignature(
		msg.Content,
		msg.Signature,
		[]byte(msg.PublicKey),
	)
	if err != nil || !valid {
		http.Error(w, fmt.Sprintf("Signature verification failed: %v", err), http.StatusBadRequest)
		return
	}

	// Check message size limits
	h.mutex.Lock()
	defer h.mutex.Unlock()

	key := fmt.Sprintf("%s:%d", msg.UtxoTxid, msg.UtxoVout)
	existingSize := 0
	if existing, ok := h.messageStore[key]; ok {
		existingSize = len(existing)
	}

	if existingSize+len(msg.Content) <= message.MaxMessageSize {
		h.messageStore[key] = msg.Content
		fmt.Printf("Message stored for UTXO %s\n", key)
		w.WriteHeader(http.StatusOK)
	} else {
		fmt.Printf("Message exceeds size limit for UTXO %s\n", key)
		http.Error(w, fmt.Sprintf("Message exceeds size limit for UTXO %s", key), http.StatusBadRequest)
	}

	// TODO: how to find peers, send packets to other peers.
}
