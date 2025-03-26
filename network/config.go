// Copyright (c) 2023 UTXOchat developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package network

// Config defines the network configuration for UTXOchat.
type Config struct {
	// ListenAddr is the address to listen on for incoming connections.
	ListenAddr string

	// Known peers to connect to on startup.
	KnownPeers []string

	// HandshakeTimeout is the timeout for peer handshake in seconds.
	HandshakeTimeout int
}

// NewDefaultConfig returns a default network configuration.
func NewDefaultConfig() Config {
	return Config{
		ListenAddr:       "0.0.0.0:8335",
		KnownPeers:       []string{},
		HandshakeTimeout: 60,
	}
}
