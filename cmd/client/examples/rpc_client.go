package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/btcsuite/btcd/rpcclient"
)

func main() {
	// Create a new RPC client configuration
	connCfg := &rpcclient.ConnConfig{
		Host:         "localhost:18443", // Regtest RPC port
		User:         "your_username",   // From bitcoin.conf
		Pass:         "your_password",   // From bitcoin.conf
		HTTPPostMode: true,              // Bitcoin core only supports HTTP POST mode
		DisableTLS:   true,              // Disable TLS for local regtest
	}

	// Create a new RPC client
	client, err := rpcclient.New(connCfg, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer client.Shutdown()

	// Handle shutdown gracefully
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Example: Get blockchain info
	client.RawRequest()
	blockChainInfo, err := client.GetBlockChainInfo()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Current block height: %d\n", blockChainInfo.Blocks)

	// Example: Get network info
	networkInfo, err := client.GetNetworkInfo()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Connected peers: %d\n", networkInfo.Connections)

	// Example: Get wallet balance
	balance, err := client.GetBalance("*")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Wallet balance: %f BTC\n", balance)

	// Example: List unspent outputs
	unspent, err := client.ListUnspent()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("\nUnspent outputs:")
	for _, utxo := range unspent {
		fmt.Printf("TxID: %s, Vout: %d, Amount: %f BTC\n",
			utxo.TxID, utxo.Vout, utxo.Amount)
	}

	// Example: Generate new blocks
	address, err := client.GetNewAddress("")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("\nGenerated new address: %s\n", address)

	// Generate 1 block
	hash, err := client.GenerateToAddress(1, address, nil)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Generated block: %s\n", hash[0])

	// Wait for shutdown signal
	<-sigChan
	fmt.Println("\nShutting down...")
}
