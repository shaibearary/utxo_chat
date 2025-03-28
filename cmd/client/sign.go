package main

import (
	"crypto/sha256"
	"fmt"
	"math"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
)

// SignMessage implements BIP322 simple signature scheme for Taproot outputs
type SignatureData struct {
	Witness     [][]byte // Witness stack for the signature
	PKScript    []byte   // The scriptPubKey being signed
	MessageHash [32]byte // Tagged hash of the message
}

func SignMessage(outpoint Outpoint, message string, privKey *btcec.PrivateKey) (*SignatureData, error) {
	// 1. Create BIP322 tagged hash of the message
	tag := []byte("BIP0322-signed-message")
	messageBytes := []byte(message)
	messageHash := sha256.Sum256(append(tag, messageBytes...))

	// 2. Create the to_spend transaction (virtual tx1)
	toSpend := wire.NewMsgTx(0) // version 0

	// Add input with zero hash and max index
	prevOut := wire.NewOutPoint(&chainhash.Hash{}, math.MaxUint32)
	txIn := wire.NewTxIn(prevOut, nil, nil)
	txIn.Sequence = 0
	toSpend.AddTxIn(txIn)

	// Add scriptSig with message hash
	builder := txscript.NewScriptBuilder()
	builder.AddOp(txscript.OP_0)
	builder.AddData(messageHash[:])
	scriptSig, _ := builder.Script()
	toSpend.TxIn[0].SignatureScript = scriptSig

	// Add output with the taproot script
	toSpend.AddTxOut(wire.NewTxOut(0, outpoint.PKScript))

	// 3. Create the to_sign transaction (virtual tx2)
	toSign := wire.NewMsgTx(0)

	// Add input spending tx1
	spendHash := toSpend.TxHash()
	txIn = wire.NewTxIn(wire.NewOutPoint(&spendHash, 0), nil, nil)
	txIn.Sequence = 0
	toSign.AddTxIn(txIn)

	// Add OP_RETURN output
	builder = txscript.NewScriptBuilder()
	builder.AddOp(txscript.OP_RETURN)
	opReturnScript, _ := builder.Script()
	toSign.AddTxOut(wire.NewTxOut(0, opReturnScript))

	// 4. Sign the transaction
	prevFetcher := txscript.NewCannedPrevOutputFetcher(outpoint.PKScript, 0)
	sigHashes := txscript.NewTxSigHashes(toSign, prevFetcher)

	// Sign with taproot key
	witness, err := txscript.TaprootWitnessSignature(
		toSign,
		sigHashes,
		0, // input index
		0, // amount (always 0 for message signing)
		outpoint.PKScript,
		txscript.SigHashDefault,
		privKey,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create signature: %v", err)
	}

	return &SignatureData{
		Witness:     witness,
		PKScript:    outpoint.PKScript,
		MessageHash: messageHash,
	}, nil
}

// Helper struct to represent a UTXO outpoint with its script
type Outpoint struct {
	TxID     [32]byte
	Vout     uint32
	PKScript []byte
}
