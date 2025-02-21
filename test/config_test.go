package test

import (
	"os"
	"testing"
	"utxo-chat/bitcoin"
	"utxo-chat/config"
)

func TestLoadConfig(t *testing.T) {
	// Create a temporary test config file
	testConfig := `{
		"rpc": {
			"host": "https://test-rpc-host.com",
			"user": "test-user",
			"pass": "test-pass"
		},
		"node": {
			"listen_addr": ":8333",
			"private_key": "deadbeef",
			"utxo_txid": "1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			"utxo_vout": 0,
			"known_peers": ["127.0.0.1:8334"]
		}
	}`

	tmpfile, err := os.CreateTemp("", "config.*.json")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpfile.Name())

	if _, err := tmpfile.Write([]byte(testConfig)); err != nil {
		t.Fatal(err)
	}
	if err := tmpfile.Close(); err != nil {
		t.Fatal(err)
	}

	// Test loading config
	cfg, err := config.LoadConfig(tmpfile.Name())
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Verify config values
	if cfg.RPC.Host != "https://test-rpc-host.com" {
		t.Errorf("Expected RPC host %s, got %s", "https://test-rpc-host.com", cfg.RPC.Host)
	}

	if cfg.Node.ListenAddr != ":8333" {
		t.Errorf("Expected listen address %s, got %s", ":8333", cfg.Node.ListenAddr)
	}

	// Test connection using config
	err = testConnection(cfg)
	if err != nil {
		t.Fatalf("Connection test failed: %v", err)
	}
}

func testConnection(cfg *config.Config) error {
	// Create a verifier with the config
	verifier, err := bitcoin.NewUtxoVerifier(
		cfg.RPC.Host,
		cfg.RPC.User,
		cfg.RPC.Pass,
	)
	if err != nil {
		return err
	}
	defer verifier.Close()

	// Try to get a test UTXO to verify connection
	_, err = verifier.VerifyUtxo(
		"1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
		0,
		"02deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
	)
	return err
}
