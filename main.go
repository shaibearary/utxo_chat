package main

import (
	"log"
	"utxo-chat/bitcoin"
	"utxo-chat/config"
	"utxo-chat/network"
)

func main() {
	config, err := config.LoadConfig("")
	if err != nil {
		log.Printf("Warning: Failed to load config file, using defaults: %v", err)
		// Continue with default configs
	}

	verifier, err := bitcoin.NewUtxoVerifier(
		config.RPC.Host,
		config.RPC.User,
		config.RPC.Pass,
	)
	if err != nil {
		log.Fatal(err)
	}

	// Convert config.NodeConfig to network.NodeConfig
	node, err := network.NewNode(network.NodeConfig{
		ListenAddr: config.Node.ListenAddr,
		PrivateKey: config.Node.PrivateKey,
		UtxoTxid:   config.Node.UtxoTxid,
		UtxoVout:   config.Node.UtxoVout,
	}, verifier)
	if err != nil {
		log.Fatal(err)
	}

	// Start the node
	if err := node.StartNode(config.Node.ListenAddr); err != nil {
		log.Fatal(err)
	}

	// Connect to known peers
	// for _, addr := range config.KnownPeers {
	// 	if err := node.ConnectToPeer(addr); err != nil {
	// 		log.Printf("Failed to connect to peer %s: %v\n", addr, err)
	// 	}
	// }

	// Example of creating and broadcasting a message
	go func() {
		msg := []byte("Hello from UTXO Chat Node!")
		if err := node.CreateAndBroadcastMessage(msg); err != nil {
			log.Printf("Failed to broadcast message: %v\n", err)
		}
	}()

}
