package bitcoin

import (
	"context"
	"fmt"

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
		DisableTLS:   true, // Note: Should be configurable in production
	}

	client, err := rpcclient.New(connCfg, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create Bitcoin client: %v", err)
	}

	return &Client{
		Client: client,
	}, nil
}

// GetBlockchainInfo retrieves the current blockchain info from the Bitcoin node.
func (c *Client) GetBlockchainInfo(ctx context.Context) (*BlockchainInfo, error) {
	// Get blockchain info using the RPC client
	info, err := c.GetBlockChainInfo()
	if err != nil {
		return nil, fmt.Errorf("failed to get blockchain info: %v", err)
	}

	return &BlockchainInfo{
		Chain:  info.Chain,
		Blocks: info.Blocks,
	}, nil
}

// Close shuts down the client.
func (c *Client) Close() {
	c.Shutdown()
}
