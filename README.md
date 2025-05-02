# UTXO Chat

This is a PoC of utxo chat proposed by [Tadge Dryja](https://github.com/adiabat). A decentralized chat system that uses Bitcoin UTXOs for spam protection without bloating the blockchain.

## Current Implementation

### ✅ What Works

1. **Basic Message Validation**
   - UTXO verification through Bitcoin RPC
   - Message signature verification (only p2pkh)
   - Message size limits (10KB per UTXO) (not test!!!!)

2. **Configuration**
   - Comprehensive JSON-based configuration
   - RPC settings for Bitcoin node
   - Network, database, and blockchain settings
   - Debug and profiling options

3. **Basic Client/Server**
   - HTTP server for message reception (should we change it to wss or other protocol) 
   - Test client for sending messages
   - In-memory message storage

### 🚧 What We Need

1. **UTXO Verification Improvements**
   - Proper verification of UTXO ownership
   - Script validation for different UTXO types
   - Better error handling and logging

2. **P2P Network Layer**
   - Peer discovery mechanism
   - Message propagation between nodes
   - Connection management
   - Peer health monitoring

3. **Data Persistence**
   - Persistent storage for messages
   - UTXO tracking
   - Message history

## Quick Start

### Configuration

1. Copy the example configuration:
```bash
cp config-example.json config.json
```

2. Edit `config.json` with your settings. Here's what each section controls:

```json
{
    "DataDir": ".utxochat",           // Directory for data storage
    "Network": {
        "ListenAddr": "0.0.0.0:8335", // Network listening address
        "KnownPeers": [],             // List of known peer addresses
        "HandshakeTimeout": 60        // Peer handshake timeout in seconds
    },
    "Bitcoin": {
        "RPCURL": "http://localhost:8332", // Bitcoin node RPC URL
        "RPCUser": "your-username",        // RPC username
        "RPCPass": "your-password",        // RPC password
        "DisableTLS": true                 // Whether to disable TLS
    },
    "Database": {
        "Type": "memory",                  // Database type (memory/leveldb)
        "Path": ".utxochat/utxochat.db"   // Database file path
    },
    "Blockchain": {
        "NotificationsEnabled": true,      // Enable block notifications
        "MaxReorgDepth": 6,               // Maximum reorg depth to handle
        "ScanFullBlocks": true,           // Whether to scan full blocks
        "PollInterval": 30                // Block polling interval in seconds
    },
    "Message": {
        "MaxPayloadSize": 65434,          // Maximum message payload size
        "MaxMessageSize": 65536           // Maximum total message size
    },
    "Debug": {
        "Profile": "",                    // HTTP profiling port
        "CPUProfile": "",                 // CPU profile output file
        "MemoryProfile": "",              // Memory profile output file
        "TraceProfile": "",               // Execution trace output file
        "LogLevel": "info"               // Logging level
    }
}
```

### Running

1. Start the server:
```bash
go run main.go
```

2. Test with client:
```bash
cd cmd/client
go run main.go -message "Your test message"
```

## Next Steps

1. **Priority 1: UTXO Verification**
   - Implement proper script validation
   - Add support for different UTXO types
   - Improve error handling

2. **Priority 2: P2P Network**
   - Design peer discovery protocol
   - Implement message propagation
   - Add connection management

3. **Priority 3: Storage**
   - Design database schema
   - Implement persistent storage
   - Add message history queries

## Contributing

# UTXO Chat Testing Environment

This project provides scripts to set up and test a Bitcoin regtest environment for UTXO chat development.

## Prerequisites

- Bitcoin Core installed and in your PATH
- Basic understanding of Bitcoin's regtest mode

## Setup

1. Make the scripts executable:
```bash
chmod +x scripts/*.sh
```

2. Start the regtest environment:
```bash
./scripts/setup_regtest.sh
```

This will:
- Create three nodes (node1, node2, node3)
- Start them with different configurations
- Connect them together
- Generate initial blocks for testing

## Testing

Use the test script to interact with the regtest environment:
```bash
./scripts/test_utxo_chat.sh
```

The script provides several functions:
- `get_new_address`: Get a new address from a node
- `get_balance`: Check wallet balance
- `send_coins`: Send coins between addresses
- `mine_blocks`: Mine new blocks

Example usage:
```bash
# Get a new address from node1
get_new_address "node1" "wallet1"

# Check balance
get_balance "node1" "wallet1"

# Mine blocks
mine_blocks "node1" 1
```

## Cleanup

When you're done testing, clean up the environment:
```bash
./scripts/cleanup_regtest.sh
```

This will:
- Stop all regtest nodes
- Remove the regtest directories

## Node Configuration

- node1: Descriptor wallet, RPC port 18443
- node2: Non-descriptor wallet, RPC port 18444
- node3: Descriptor wallet, RPC port 18445

## Troubleshooting

If you encounter issues:
1. Check if Bitcoin Core is properly installed
2. Ensure no other regtest nodes are running
3. Check the regtest directories for any leftover files
4. Try running the cleanup script before starting fresh
