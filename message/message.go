package message

type ChatMessage struct {
    Content    []byte `json:"content"`
    Signature  []byte `json:"signature"`
    PublicKey  string `json:"public_key"`
    UtxoTxid   string `json:"utxo_txid"`
    UtxoVout   uint32 `json:"utxo_vout"`
}

const MaxMessageSize = 1024 * 10 // 10KB limit per UTXO 