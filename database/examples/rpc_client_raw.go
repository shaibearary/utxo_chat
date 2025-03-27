package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

type RPCRequest struct {
	Jsonrpc string        `json:"jsonrpc"`
	ID      int           `json:"id"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params"`
}

type RPCResponse struct {
	Jsonrpc string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *RPCError       `json:"error,omitempty"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func main() {
	// RPC configuration
	rpcURL := "http://localhost:18443"
	rpcUser := "utxochat"
	rpcPass := "utxochat123"

	// Create HTTP client
	client := &http.Client{}

	// Handle shutdown gracefully
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Example: Get blockchain info
	blockChainInfo, err := makeRPCRequest(client, rpcURL, rpcUser, rpcPass, "getblockchaininfo", []interface{}{})
	if err != nil {
		log.Printf("Warning: getblockchaininfo failed: %v", err)
	} else {
		var result struct {
			Blocks int `json:"blocks"`
		}
		if err := json.Unmarshal(blockChainInfo, &result); err != nil {
			log.Printf("Warning: failed to parse blockchain info: %v", err)
		} else {
			fmt.Printf("Current block height: %d\n", result.Blocks)
		}
	}

	// Example: Get network info
	networkInfo, err := makeRPCRequest(client, rpcURL, rpcUser, rpcPass, "getnetworkinfo", []interface{}{})
	if err != nil {
		log.Printf("Warning: getnetworkinfo failed: %v", err)
	} else {
		var result struct {
			Connections int `json:"connections"`
		}
		if err := json.Unmarshal(networkInfo, &result); err != nil {
			log.Printf("Warning: failed to parse network info: %v", err)
		} else {
			fmt.Printf("Connected peers: %d\n", result.Connections)
		}
	}

	// Example: Get wallet balance
	balance, err := makeRPCRequest(client, rpcURL, rpcUser, rpcPass, "getbalance", []interface{}{"*"})
	if err != nil {
		log.Printf("Warning: getbalance failed: %v", err)
	} else {
		var result float64
		if err := json.Unmarshal(balance, &result); err != nil {
			log.Printf("Warning: failed to parse balance: %v", err)
		} else {
			fmt.Printf("Wallet balance: %f BTC\n", result)
		}
	}

	// Example: List unspent outputs
	unspent, err := makeRPCRequest(client, rpcURL, rpcUser, rpcPass, "listunspent", []interface{}{})
	if err != nil {
		log.Printf("Warning: listunspent failed: %v", err)
	} else {
		var result []struct {
			TxID   string  `json:"txid"`
			Vout   int     `json:"vout"`
			Amount float64 `json:"amount"`
		}
		if err := json.Unmarshal(unspent, &result); err != nil {
			log.Printf("Warning: failed to parse unspent outputs: %v", err)
		} else {
			fmt.Println("\nUnspent outputs:")
			for _, utxo := range result {
				fmt.Printf("TxID: %s, Vout: %d, Amount: %f BTC\n",
					utxo.TxID, utxo.Vout, utxo.Amount)
			}
		}
	}

	// Example: Generate new blocks
	address, err := makeRPCRequest(client, rpcURL, rpcUser, rpcPass, "getnewaddress", []interface{}{""})
	if err != nil {
		log.Printf("Warning: getnewaddress failed: %v", err)
	} else {
		var result string
		if err := json.Unmarshal(address, &result); err != nil {
			log.Printf("Warning: failed to parse address: %v", err)
		} else {
			fmt.Printf("\nGenerated new address: %s\n", result)

			// Generate 1 block
			hash, err := makeRPCRequest(client, rpcURL, rpcUser, rpcPass, "generatetoaddress", []interface{}{1, result})
			if err != nil {
				log.Printf("Warning: generatetoaddress failed: %v", err)
			} else {
				var result []string
				if err := json.Unmarshal(hash, &result); err != nil {
					log.Printf("Warning: failed to parse block hash: %v", err)
				} else if len(result) > 0 {
					fmt.Printf("Generated block: %s\n", result[0])
				}
			}
		}
	}

	// Wait for shutdown signal
	<-sigChan
	fmt.Println("\nShutting down...")
}

func makeRPCRequest(client *http.Client, url, user, pass, method string, params []interface{}) (json.RawMessage, error) {
	req := RPCRequest{
		Jsonrpc: "2.0",
		ID:      1,
		Method:  method,
		Params:  params,
	}

	jsonData, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}

	httpReq.SetBasicAuth(user, pass)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var rpcResp RPCResponse
	if err := json.Unmarshal(body, &rpcResp); err != nil {
		return nil, err
	}

	if rpcResp.Error != nil {
		return nil, fmt.Errorf("RPC error: %s", rpcResp.Error.Message)
	}

	return rpcResp.Result, nil
}
