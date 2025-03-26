// Copyright (c) 2023 UTXOchat developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package blockchain

// Config holds configuration options for the blockchain handler.
type Config struct {
	// NotificationsEnabled specifies whether to enable block notifications.
	NotificationsEnabled bool

	// MaxReorgDepth specifies the maximum number of blocks to keep track of
	// for potential chain reorganizations.
	MaxReorgDepth int32

	// ScanFullBlocks determines whether the handler should process full block data
	// or just headers.
	ScanFullBlocks bool

	// PollInterval specifies the interval in seconds between block polling attempts
	// when notifications are disabled.
	PollInterval int
}

// DefaultConfig returns the default configuration for the blockchain handler.
func DefaultConfig() Config {
	return Config{
		NotificationsEnabled: true,
		MaxReorgDepth:        6,
		ScanFullBlocks:       true,
		PollInterval:         30,
	}
}
