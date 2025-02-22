# UTXO Chat

This is a PoC of utxo chat proposed by [Tadge Dryja](https://github.com/adiabat). A decentralized chat system that uses Bitcoin UTXOs for spam protection without bloating the blockchain.

## Current Implementation

### ✅ What Works

1. **Basic Message Validation**
   - UTXO verification through Bitcoin RPC
   - Message signature verification (only p2pkh)
   - Message size limits (10KB per UTXO) (not test!!!!)

2. **Configuration**
   - JSON-based configuration
   - RPC settings for Bitcoin node
   - Node configuration (listen address, keys, UTXOs)

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

1. Copy the default configuration:
```bash
cp config/default.json config/config.json
```

2. Edit `config/config.json` with your Bitcoin RPC settings:
```json
{
    "rpc": {
        "host": "your-bitcoin-rpc-host",
        "user": "your-rpc-username",
        "pass": "your-rpc-password"
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
