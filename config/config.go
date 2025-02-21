package config

import (
	"encoding/json"
	"os"
)

type Config struct {
	RPC  RpcConfig  `json:"rpc"`
	Node NodeConfig `json:"node"`
}

type RpcConfig struct {
	Host string `json:"host"`
	User string `json:"user"`
	Pass string `json:"pass"`
}

type NodeConfig struct {
	ListenAddr string   `json:"listen_addr"`
	PrivateKey string   `json:"private_key"`
	UtxoTxid   string   `json:"utxo_txid"`
	UtxoVout   uint32   `json:"utxo_vout"`
	KnownPeers []string `json:"known_peers"`
}

func LoadConfig(configPath string) (*Config, error) {
	if configPath == "" {
		configPath = "config/config.json"
	}

	file, err := os.Open(configPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	config := &Config{}
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(config); err != nil {
		return nil, err
	}

	return config, nil
}


