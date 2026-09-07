// UTXO Chat - A decentralized messaging system using Bitcoin UTXOs
// Copyright (C) 2024 UTXO Chat developers
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package main

import (
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"math"
	"net"

	"github.com/shaibearary/utxo_chat/signer"
)

func main() {
	// Command line flags
	descriptor := flag.String("descriptor", "", "Taproot private descriptor (test keys only)")
	index := flag.Uint("index", 0, "Address index substituted for a wildcard (*) in the descriptor path")
	txid := flag.String("txid", "", "Transaction ID")
	vout := flag.Uint("vout", 0, "Output index")
	message := flag.String("message", "", "Message to sign")
	server := flag.String("server", "127.0.0.1:8335", "UTXO Chat node TCP address")
	flag.Parse()
	if *index > math.MaxUint32 {
		log.Fatal("-index is out of range")
	}
	if *descriptor == "" || *txid == "" || *message == "" {
		log.Fatal("-descriptor, -txid, and -message are required; use a disposable testnet descriptor only")
	}

	var outpoint signer.Outpoint
	txidBytes, err := hex.DecodeString(*txid)
	if err != nil || len(txidBytes) != 32 {
		log.Fatal("-txid must be exactly 64 hexadecimal characters")
	}
	copy(outpoint.TxID[:], txidBytes)
	outpoint.Index = uint32(*vout)

	// Sign message
	msg, err := signer.SignMessageWithTaproot(*descriptor, uint32(*index), outpoint, *message)
	if err != nil {
		log.Fatalf("Error signing message: %v", err)
	}

	// Connect to the UTXO Chat server
	conn, err := net.Dial("tcp", *server)
	if err != nil {
		log.Fatalf("Failed to connect to server: %v", err)
	}
	defer conn.Close()

	// Prepare message with type header (MessageTypeData = 0x03)
	fullMsg := make([]byte, 0, 1+len(msg))
	fullMsg = append(fullMsg, signer.MessageTypeData)
	fullMsg = append(fullMsg, msg...)

	// Send the message
	_, err = conn.Write(fullMsg)
	if err != nil {
		log.Fatalf("Failed to send message: %v", err)
	}

	fmt.Printf("Successfully sent %d bytes.\n", len(fullMsg))
	fmt.Printf("Submitted message to %s for outpoint %s:%d. The current P2P protocol has no acknowledgement.\n", *server, *txid, *vout)
}
