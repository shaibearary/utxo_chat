package bitcoin

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/rpcclient"
)

// Config defines the Bitcoin node configuration.
type Config struct {
	RPCURL  string
	RPCUser string
	RPCPass string
}

// Client represents a Bitcoin RPC client.
type Client struct {
	*rpcclient.Client
}

// BlockchainInfo represents the response from getblockchaininfo RPC call.
type BlockchainInfo struct {
	Chain  string `json:"chain"`
	Blocks int32  `json:"blocks"`
}

// NewClient creates a new Bitcoin RPC client.
func NewClient(cfg Config) (*Client, error) {
	// Parse host from RPCURL
	host := cfg.RPCURL
	if host == "" {
		host = "localhost:8332"
	}

	connCfg := &rpcclient.ConnConfig{
		Host:         host,
		User:         cfg.RPCUser,
		Pass:         cfg.RPCPass,
		HTTPPostMode: true,
		DisableTLS:   true,
	}
	fmt.Println("connCfg", connCfg)
	client, err := rpcclient.New(connCfg, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create Bitcoin client: %v", err)
	}

	return &Client{
		Client: client,
	}, nil
}

type GetBlockchainInfoResult struct {
	RegtestResult *RegtestGetBlockchainInfoResult
	MainnetResult *btcjson.GetBlockChainInfoResult
	IsRegtest     bool
}
type RegtestGetBlockchainInfoResult struct {
	// ... other fields ...
	Chain                string   `json:"chain"`
	Blocks               int32    `json:"blocks"`
	Headers              int32    `json:"headers"`
	BestBlockHash        string   `json:"bestblockhash"`
	Difficulty           float64  `json:"difficulty"`
	MedianTime           int64    `json:"mediantime"`
	VerificationProgress float64  `json:"verificationprogress"`
	InitialBlockDownload bool     `json:"initialblockdownload"`
	Chainwork            string   `json:"chainwork"`
	SizeOnDisk           int64    `json:"size_on_disk"`
	Pruned               bool     `json:"pruned"`
	Warnings             []string `json:"warnings"`
}

// GetBlockchainInfo retrieves the current blockchain info from the Bitcoin node.
func (c *Client) GetBlockchainInfo(ctx context.Context) (*BlockchainInfo, error) {
	// Get blockchain info using the RPC client
	result, err := c.RawRequest("getblockchaininfo", []json.RawMessage{})
	if err != nil {
		return nil, fmt.Errorf("failed to get blockchain info: %v", err)
	}

	// Unmarshal into map to see all fields
	var rawInfo map[string]interface{}
	if err := json.Unmarshal(result, &rawInfo); err != nil {
		return nil, fmt.Errorf("failed to parse raw info: %v", err)
	}

	// Print all fields and their types for debugging
	for key, value := range rawInfo {
		fmt.Printf("Field: %s, Type: %T, Value: %v\n", key, value, value)
	}

	// Convert raw info to BlockchainInfo
	chain, _ := rawInfo["chain"].(string)
	blocks, _ := rawInfo["blocks"].(float64)

	return &BlockchainInfo{
		Chain:  chain,
		Blocks: int32(blocks),
	}, nil
}

// Close shuts down the client.
func (c *Client) Close() {
	c.Shutdown()
}
