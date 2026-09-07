package rpc

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"
	"unicode/utf8"

	"github.com/shaibearary/utxo_chat/message"
	"github.com/shaibearary/utxo_chat/network"
	"github.com/shaibearary/utxo_chat/signer"
)

// Server handles RPC requests for UTXOchat
type Server struct {
	networkManager *network.Manager
	listenAddr     string

	// chain is the network the attached Bitcoin node reports. Signing
	// endpoints are refused unless it is "regtest", because they accept a
	// private descriptor over HTTP.
	chain string
}

// NewServer creates a new RPC server
func NewServer(networkManager *network.Manager, listenAddr, chain string) *Server {
	return &Server{
		networkManager: networkManager,
		listenAddr:     listenAddr,
		chain:          chain,
	}
}

// Listen binds the RPC port and registers the routes.
//
// Binding is separated from serving so a caller can report a bind failure
// before it announces the server as running. When the two were one call the
// only place the error could surface was a goroutine nobody waited on, so a
// node whose port was already taken logged the failure once and then logged
// that its API was up. Anything talking to that node saw connection refused
// against a process that looked healthy.
func (s *Server) Listen() (net.Listener, error) {
	http.HandleFunc("/sendmessage", s.handleSendMessage)
	http.HandleFunc("/getmessages", s.handleGetMessages)
	http.HandleFunc("/getmessage", s.handleGetMessage)
	http.HandleFunc("/composemessage", s.handleComposeMessage)
	http.HandleFunc("/submitsigned", s.handleSubmitSigned)
	http.HandleFunc("/monitor", s.handleMonitor)

	ln, err := net.Listen("tcp", s.listenAddr)
	if err != nil {
		return nil, fmt.Errorf("cannot bind RPC address %s: %w", s.listenAddr, err)
	}
	return ln, nil
}

// Serve handles requests on an already bound listener until it fails.
func (s *Server) Serve(ln net.Listener) error {
	log.Printf("Starting RPC server on %s", ln.Addr())
	return http.Serve(ln, nil)
}

// Start binds and serves in one call, for callers with nothing to do in
// between.
func (s *Server) Start() error {
	ln, err := s.Listen()
	if err != nil {
		return err
	}
	return s.Serve(ln)
}

// handleMonitor serves a dependency-free local dashboard. It polls the public
// message listing so operators can observe messages arriving and disappearing
// without installing a separate frontend.
func (s *Server) handleMonitor(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// The console is compiled into the binary, so a rebuilt node serves a new
	// page at an unchanged URL. Without this a browser keeps the old copy and
	// the operator debugs behaviour the node no longer has, which cost real
	// time when a cached page rendered a field the current code cannot emit.
	w.Header().Set("Cache-Control", "no-store, must-revalidate")
	fmt.Fprint(w, monitorPage)
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
	err = s.networkManager.ProcessMessage(context.Background(), msgData, network.MessageSourceLocal, "")
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

	// Chain lets a client tell a regtest sandbox from a live node without a
	// second request; the console uses it to decide whether to offer signing.
	Chain string `json:"chain,omitempty"`

	// Node is this node's own peer listen address. With several nodes on one
	// machine the console pages look identical, and the origin of a message
	// only means something once you know which node is reporting it.
	Node string `json:"node,omitempty"`
}

// OriginInfo says how the reporting node came to hold a message. It is omitted
// entirely for messages this process never saw arrive, which is every message
// reloaded from disk after a restart.
type OriginInfo struct {
	// Source is "peer" for a relayed message, or "local"/"wallet" for one
	// submitted through this node's own API. It is empty when this node has
	// only heard the outpoint advertised and already had the body, which is
	// what every peer announcement looks like after the first round.
	Source string `json:"source,omitempty"`

	// Peer is the address the message was received from, empty when local.
	Peer string `json:"peer,omitempty"`

	// Announcers lists every peer that advertised this outpoint.
	Announcers []string `json:"announcers,omitempty"`

	FirstSeen string `json:"first_seen,omitempty"`
}

// MessageInfo represents information about a stored message. The payload is
// included so a client can render a conversation from one poll instead of
// issuing a follow-up request per outpoint.
type MessageInfo struct {
	Outpoint string `json:"outpoint"`
	Size     int    `json:"size"`

	// Payload holds the message text when it is valid UTF-8. IsText says
	// whether it does; PayloadHex is always populated so a caller can render
	// arbitrary bytes, since the protocol does not require text.
	Payload    string `json:"payload"`
	PayloadHex string `json:"payload_hex"`
	IsText     bool   `json:"is_text"`

	// Origin says which peer relayed this message here, when this node saw it
	// arrive. Nil when it did not.
	Origin *OriginInfo `json:"origin,omitempty"`
}

// describeMessage loads one stored message and renders it for the API. A
// missing or corrupt record yields an entry that reports the outpoint with a
// zero size rather than failing the whole listing, so one bad record cannot
// blank the operator console.
func (s *Server) describeMessage(ctx context.Context, outpoint message.Outpoint) MessageInfo {
	info := MessageInfo{Outpoint: outpoint.ToString()}

	if origin, ok := s.networkManager.MessageOrigin(outpoint); ok {
		info.Origin = &OriginInfo{
			Peer:       origin.Peer,
			Announcers: origin.Announcers,
			FirstSeen:  origin.FirstSeen.UTC().Format(time.RFC3339),
		}
		if origin.Delivered {
			info.Origin.Source = origin.Source.String()
		}
	}

	raw, err := s.networkManager.GetMessage(ctx, outpoint)
	if err != nil {
		log.Printf("monitor: cannot read message %s: %v", info.Outpoint, err)
		return info
	}

	msg, err := message.Deserialize(raw)
	if err != nil {
		log.Printf("monitor: cannot decode message %s: %v", info.Outpoint, err)
		return info
	}

	info.Size = len(msg.Payload)
	info.PayloadHex = hex.EncodeToString(msg.Payload)
	if utf8.Valid(msg.Payload) {
		info.Payload = string(msg.Payload)
		info.IsText = true
	}

	return info
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
	ctx := r.Context()
	messages := make([]MessageInfo, len(outpoints))
	for i, outpoint := range outpoints {
		messages[i] = s.describeMessage(ctx, outpoint)
	}

	response := GetMessagesResponse{
		Messages: messages,
		Count:    len(messages),
		Chain:    s.chain,
		Node:     s.networkManager.ListenAddr(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleGetMessage returns a single stored message addressed by its textual
// outpoint, for clients that already know which one they want.
func (s *Server) handleGetMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	outpoint, err := message.ParseOutpoint(r.URL.Query().Get("outpoint"))
	if err != nil {
		s.sendErrorResponse(w, fmt.Sprintf("Invalid outpoint: %v", err))
		return
	}

	// Ask the database directly so a genuinely absent message is a 404 rather
	// than an entry with an empty payload.
	if _, err := s.networkManager.GetMessage(r.Context(), outpoint); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(SendMessageResponse{Error: "No message stored for that outpoint"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.describeMessage(r.Context(), outpoint))
}

// ComposeMessageRequest asks the node to sign a message on the caller's behalf.
type ComposeMessageRequest struct {
	Descriptor string `json:"descriptor"`
	Index      uint32 `json:"index"`
	TxID       string `json:"txid"`
	Vout       uint32 `json:"vout"`
	Message    string `json:"message"`
}

// ComposeMessageResponse reports the outcome of a compose request.
type ComposeMessageResponse struct {
	Success  bool   `json:"success"`
	Error    string `json:"error,omitempty"`
	Outpoint string `json:"outpoint,omitempty"`
}

// handleComposeMessage signs a message with a supplied private descriptor and
// publishes it. This accepts a private key over a local HTTP request, so it is
// restricted to regtest, where the keys are disposable by construction. On any
// other chain it is refused outright regardless of what the caller sends.
func (s *Server) handleComposeMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.chain != "regtest" {
		s.sendComposeError(w, http.StatusForbidden,
			fmt.Sprintf("Signing over RPC is only available on regtest; this node is on %q", s.chain))
		return
	}

	var req ComposeMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.sendComposeError(w, http.StatusBadRequest, fmt.Sprintf("Invalid request: %v", err))
		return
	}

	if req.Descriptor == "" || req.TxID == "" || req.Message == "" {
		s.sendComposeError(w, http.StatusBadRequest, "descriptor, txid, and message are required")
		return
	}

	txid, err := hex.DecodeString(req.TxID)
	if err != nil || len(txid) != 32 {
		s.sendComposeError(w, http.StatusBadRequest, "txid must be exactly 64 hexadecimal characters")
		return
	}

	var outpoint signer.Outpoint
	copy(outpoint.TxID[:], txid)
	outpoint.Index = req.Vout

	raw, err := signer.SignMessageWithTaproot(req.Descriptor, req.Index, outpoint, req.Message)
	if err != nil {
		s.sendComposeError(w, http.StatusBadRequest, fmt.Sprintf("Failed to sign: %v", err))
		return
	}

	// The local path is the same one the CLI reaches over TCP, so a message
	// composed here is validated and relayed identically.
	if err := s.networkManager.ProcessMessage(r.Context(), raw, network.MessageSourceLocal, ""); err != nil {
		s.sendComposeError(w, http.StatusBadRequest, fmt.Sprintf("Node rejected the message: %v", err))
		return
	}

	stored, err := message.Deserialize(raw)
	if err != nil {
		s.sendComposeError(w, http.StatusInternalServerError, fmt.Sprintf("Signed an undecodable message: %v", err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ComposeMessageResponse{
		Success:  true,
		Outpoint: stored.Outpoint.ToString(),
	})
}

// sendComposeError reports a compose failure with a status the client can act
// on, rather than collapsing every cause into 400.
func (s *Server) sendComposeError(w http.ResponseWriter, status int, errorMsg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(ComposeMessageResponse{Success: false, Error: errorMsg})
}

// SubmitSignedRequest carries a message that was signed by an external wallet.
// It is deliberately the mirror image of ComposeMessageRequest: the caller
// keeps its keys and sends only the signature, so there is no descriptor field
// and nothing secret crosses the wire.
type SubmitSignedRequest struct {
	TxID      string `json:"txid"`
	Vout      uint32 `json:"vout"`
	Signature string `json:"signature"`
	Payload   string `json:"payload"`
}

// handleSubmitSigned publishes a message signed by the operator's own wallet.
//
// Unlike /composemessage this is available on every chain. That endpoint is
// restricted to regtest because it accepts a private descriptor over HTTP;
// here the node never sees a key, so the regtest guard would only stop a
// mainnet operator from using the one path that is actually safe for them.
func (s *Server) handleSubmitSigned(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req SubmitSignedRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.sendComposeError(w, http.StatusBadRequest, fmt.Sprintf("Invalid request: %v", err))
		return
	}

	if req.TxID == "" || req.Signature == "" {
		s.sendComposeError(w, http.StatusBadRequest, "txid and signature are required")
		return
	}

	// The payload is passed through exactly as received. Trimming it here
	// would change the BIP-322 digest the wallet signed and turn a valid
	// signature into an unexplainable rejection.
	raw, err := message.BuildSigned(req.TxID, req.Vout, req.Signature, []byte(req.Payload))
	if err != nil {
		s.sendComposeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// MessageSourceWallet, not MessageSourceLocal: the node did not produce
	// this signature and must verify it against the UTXO before accepting.
	if err := s.networkManager.ProcessMessage(r.Context(), raw, network.MessageSourceWallet, ""); err != nil {
		s.sendComposeError(w, http.StatusBadRequest, fmt.Sprintf("Node rejected the message: %v", err))
		return
	}

	stored, err := message.Deserialize(raw)
	if err != nil {
		s.sendComposeError(w, http.StatusInternalServerError, fmt.Sprintf("Assembled an undecodable message: %v", err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ComposeMessageResponse{
		Success:  true,
		Outpoint: stored.Outpoint.ToString(),
	})
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
