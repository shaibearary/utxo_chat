package main

import (
	"bytes"
	"encoding/hex"
	"testing"

	"github.com/shaibearary/utxo_chat/database"
	"github.com/shaibearary/utxo_chat/message"
)

// testDescriptor is a disposable regtest master key with the BIP-86 path that
// Bitcoin Core produces for a Taproot wallet. It holds no value on any network.
const testDescriptor = "tr(tprv8ZgxMBicQKsPeFLRgqA9SXoKiPQw7uGU1L9sDWFrQoxX4ryJhrK882ukG1SPjJfkesSJszdAfvhyByj895XD9pcASiGYhPPqV7Aw1riYopg/86h/1h/0h/0/*)#zt7ufkql"

// testOutpoint is only carried through the message; nothing in signing or
// signature verification depends on its value.
var testOutpoint = Outpoint{Index: 0}

// signedMessage signs payload at the given address index and returns the
// serialized message plus the Taproot script the signature commits to.
func signedMessage(t *testing.T, index uint32, payload string) (*message.Message, []byte) {
	t.Helper()

	_, script, err := deriveTaprootKey(testDescriptor, index)
	if err != nil {
		t.Fatalf("deriveTaprootKey(index=%d) returned an error: %v", index, err)
	}

	raw, err := SignMessageWithTaproot(testDescriptor, index, testOutpoint, payload)
	if err != nil {
		t.Fatalf("SignMessageWithTaproot(index=%d) returned an error: %v", index, err)
	}

	msg, err := message.Deserialize(raw)
	if err != nil {
		t.Fatalf("the signer produced a message the node cannot deserialize: %v", err)
	}

	return msg, script
}

// TestParseTaprootDescriptorDerivesTheCompletePath is the regression test for
// the defect where the path was sliced as parts[1:len(parts)-1], dropping the
// final element and deriving the parent of the intended key.
func TestParseTaprootDescriptorDerivesTheCompletePath(t *testing.T) {
	const hardened = 0x80000000

	tests := []struct {
		name       string
		descriptor string
		index      uint32
		wantPath   []uint32
	}{
		{
			name:       "wildcard resolves to the requested index",
			descriptor: "tr(tprv/86h/1h/0h/0/*)#checksum",
			index:      7,
			wantPath:   []uint32{86 + hardened, 1 + hardened, 0 + hardened, 0, 7},
		},
		{
			name:       "explicit final index is kept",
			descriptor: "tr(tprv/86h/1h/0h/0/5)#checksum",
			index:      0,
			wantPath:   []uint32{86 + hardened, 1 + hardened, 0 + hardened, 0, 5},
		},
		{
			name:       "apostrophe marks hardened elements too",
			descriptor: "tr(tprv/86'/1'/0'/0/*)#checksum",
			index:      3,
			wantPath:   []uint32{86 + hardened, 1 + hardened, 0 + hardened, 0, 3},
		},
		{
			name:       "key origin block is ignored",
			descriptor: "tr([5844d934/86h/1h/0h]tprv/0/*)#checksum",
			index:      2,
			wantPath:   []uint32{0, 2},
		},
		{
			name:       "descriptor without a checksum is accepted",
			descriptor: "tr(tprv/86h/1h/0h/0/1)",
			index:      0,
			wantPath:   []uint32{86 + hardened, 1 + hardened, 0 + hardened, 0, 1},
		},
		{
			name:       "a bare key has an empty path",
			descriptor: "tr(tprv)#checksum",
			index:      0,
			wantPath:   []uint32{},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			key, path, err := parseTaprootDescriptor(test.descriptor, test.index)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if key != "tprv" {
				t.Errorf("extended key = %q, want %q", key, "tprv")
			}
			if len(path) != len(test.wantPath) {
				t.Fatalf("path = %v, want %v", path, test.wantPath)
			}
			for i := range path {
				if path[i] != test.wantPath[i] {
					t.Fatalf("path = %v, want %v", path, test.wantPath)
				}
			}
		})
	}
}

func TestParseTaprootDescriptorRejectsMalformedInput(t *testing.T) {
	tests := []struct {
		name       string
		descriptor string
	}{
		{"not a taproot descriptor", "wpkh(tprv/84h/1h/0h/0/0)#checksum"},
		{"missing closing parenthesis", "tr(tprv/86h/1h/0h/0/0"},
		{"script path descriptor", "tr(tprv/86h/1h/0h/0/0,{pk(a),pk(b)})#checksum"},
		{"unterminated key origin", "tr([5844d934/86htprv/0/*)#checksum"},
		{"missing extended key", "tr(/86h/1h/0h/0/0)#checksum"},
		{"non-numeric path element", "tr(tprv/86h/1h/0h/0/abc)#checksum"},
		{"empty path element", "tr(tprv/86h//0h/0/0)#checksum"},
		{"index out of range", "tr(tprv/86h/1h/0h/0/4294967295)#checksum"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, _, err := parseTaprootDescriptor(test.descriptor, 0); err == nil {
				t.Fatalf("parseTaprootDescriptor(%q) succeeded, want an error", test.descriptor)
			}
		})
	}
}

// TestDerivedScriptMatchesBitcoinCore pins the derivation against a Taproot
// scriptPubKey that Bitcoin Core generated for this descriptor at index 0. If
// the path handling regresses, this fails.
func TestDerivedScriptMatchesBitcoinCore(t *testing.T) {
	const wantHex = "5120c31348066dfe31bdce89450f1f25cf2d7f3b2d89225dfaaf613f68f09ca0ca03"

	_, script, err := deriveTaprootKey(testDescriptor, 0)
	if err != nil {
		t.Fatalf("deriveTaprootKey returned an error: %v", err)
	}

	if got := hex.EncodeToString(script); got != wantHex {
		t.Errorf("derived scriptPubKey = %s, want %s", got, wantHex)
	}
}

// TestValidatorAcceptsSignatureFromSigner is the case that matters: a message
// produced by the signer must satisfy the node's validator.
func TestValidatorAcceptsSignatureFromSigner(t *testing.T) {
	msg, script := signedMessage(t, 0, "hello from the regtest signer")

	// VerifySignature touches neither the Bitcoin client nor the database.
	validator := database.NewValidator(nil, nil)

	err := validator.VerifySignature(string(msg.Payload), msg.Signature[:], script)
	if err != nil {
		t.Fatalf("the validator rejected a signature from the signer: %v", err)
	}
}

// TestValidatorRejectsWrongKey covers the defect that motivated this work: a
// signature made with the wrong derived key must be rejected, not merely logged.
func TestValidatorRejectsWrongKey(t *testing.T) {
	const payload = "hello from the regtest signer"

	// Sign at index 1 but present the script of the index 0 address, which is
	// what happens when the signer derives a key the UTXO does not belong to.
	msg, _ := signedMessage(t, 1, payload)
	_, script := signedMessage(t, 0, payload)

	validator := database.NewValidator(nil, nil)

	if err := validator.VerifySignature(payload, msg.Signature[:], script); err == nil {
		t.Fatal("the validator accepted a signature made with the wrong key")
	}
}

func TestValidatorRejectsTamperedSignature(t *testing.T) {
	const payload = "hello from the regtest signer"

	msg, script := signedMessage(t, 0, payload)

	forged := msg.Signature
	forged[0] ^= 0xff

	validator := database.NewValidator(nil, nil)

	if err := validator.VerifySignature(payload, forged[:], script); err == nil {
		t.Fatal("the validator accepted a tampered signature")
	}
}

// TestValidatorRejectsAlteredPayload proves the signature commits to the
// message body, so a relaying peer cannot rewrite the text.
func TestValidatorRejectsAlteredPayload(t *testing.T) {
	msg, script := signedMessage(t, 0, "the original message")

	validator := database.NewValidator(nil, nil)

	if err := validator.VerifySignature("the altered message", msg.Signature[:], script); err == nil {
		t.Fatal("the validator accepted a signature over different text")
	}
}

// TestSignedMessageRoundTrips checks the signer emits what the node's codec
// expects: a 64-byte signature and an intact payload.
func TestSignedMessageRoundTrips(t *testing.T) {
	const payload = "round trip"

	msg, _ := signedMessage(t, 0, payload)

	if !bytes.Equal(msg.Payload, []byte(payload)) {
		t.Errorf("payload = %q, want %q", msg.Payload, payload)
	}
	if int(msg.Length) != len(payload) {
		t.Errorf("length field = %d, want %d", msg.Length, len(payload))
	}
	if msg.Signature == ([64]byte{}) {
		t.Error("the message carries an empty signature")
	}
}
