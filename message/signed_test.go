package message

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"strings"
	"testing"
)

func testSig() []byte {
	sig := make([]byte, SignatureSize)
	for i := range sig {
		sig[i] = byte(i)
	}
	return sig
}

// BuildSigned is the only path a wallet-signed message takes into the node, so
// its output has to deserialize back into exactly what the caller supplied.
func TestBuildSignedRoundTrips(t *testing.T) {
	txid := strings.Repeat("ab", 32)
	sig := testSig()
	payload := []byte("hello from a wallet")

	raw, err := BuildSigned(txid, 7, hex.EncodeToString(sig), payload)
	if err != nil {
		t.Fatalf("BuildSigned: %v", err)
	}

	msg, err := Deserialize(raw)
	if err != nil {
		t.Fatalf("Deserialize: %v", err)
	}
	if got := hex.EncodeToString(msg.Outpoint[:32]); got != txid {
		t.Errorf("txid = %s, want %s", got, txid)
	}
	if got := binary.LittleEndian.Uint32(msg.Outpoint[32:36]); got != 7 {
		t.Errorf("vout = %d, want 7", got)
	}
	if string(msg.Payload) != string(payload) {
		t.Errorf("payload = %q, want %q", msg.Payload, payload)
	}
	if string(msg.Signature[:]) != string(sig) {
		t.Error("signature did not survive the round trip")
	}
}

// Sparrow copies base64 while command line tooling prints hex. Both must
// produce the same message or a user cannot tell why one was rejected.
func TestBuildSignedAcceptsHexAndBase64(t *testing.T) {
	txid := strings.Repeat("cd", 32)
	sig := testSig()

	fromHex, err := BuildSigned(txid, 0, hex.EncodeToString(sig), []byte("x"))
	if err != nil {
		t.Fatalf("hex signature: %v", err)
	}
	fromBase64, err := BuildSigned(txid, 0, base64.StdEncoding.EncodeToString(sig), []byte("x"))
	if err != nil {
		t.Fatalf("base64 signature: %v", err)
	}
	if string(fromHex) != string(fromBase64) {
		t.Error("hex and base64 signatures produced different messages")
	}
}

// The payload is what was signed, so BuildSigned must not normalize it. If it
// trimmed the trailing space here the BIP-322 digest would no longer match.
func TestBuildSignedPreservesPayloadExactly(t *testing.T) {
	raw, err := BuildSigned(strings.Repeat("11", 32), 0, hex.EncodeToString(testSig()), []byte("  padded  "))
	if err != nil {
		t.Fatalf("BuildSigned: %v", err)
	}
	msg, err := Deserialize(raw)
	if err != nil {
		t.Fatalf("Deserialize: %v", err)
	}
	if string(msg.Payload) != "  padded  " {
		t.Errorf("payload = %q, want %q", msg.Payload, "  padded  ")
	}
}

func TestBuildSignedRejectsBadInput(t *testing.T) {
	goodTxid := strings.Repeat("ab", 32)
	goodSig := hex.EncodeToString(testSig())

	cases := []struct {
		name, txid, sig string
	}{
		{"short txid", strings.Repeat("ab", 31), goodSig},
		{"non-hex txid", strings.Repeat("zz", 32), goodSig},
		{"short signature", goodTxid, hex.EncodeToString(testSig()[:63])},
		{"garbage signature", goodTxid, "not-a-signature"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := BuildSigned(tc.txid, 0, tc.sig, []byte("x")); err == nil {
				t.Error("expected an error, got nil")
			}
		})
	}
}

func TestBuildSignedRejectsOversizedPayload(t *testing.T) {
	if _, err := BuildSigned(strings.Repeat("ab", 32), 0, hex.EncodeToString(testSig()), make([]byte, MaxPayloadSize+1)); err != ErrMessageTooLarge {
		t.Errorf("err = %v, want %v", err, ErrMessageTooLarge)
	}
}

// Sparrow's "BIP322 (Simple)" output is a base64 witness stack, not a bare
// signature. Every encoding below carries the same 64 signature bytes and must
// decode to them; rejecting any one of them tells a user their wallet's own
// output is invalid.
func TestDecodeSignatureAcceptsWalletEncodings(t *testing.T) {
	sig := testSig()

	witnessDefault := append([]byte{0x01, 0x40}, sig...)           // SIGHASH_DEFAULT
	witnessAll := append(append([]byte{0x01, 0x41}, sig...), 0x01) // SIGHASH_ALL
	bareWithFlag := append(append([]byte{}, sig...), 0x01)

	cases := []struct {
		name, text string
	}{
		{"bare hex", hex.EncodeToString(sig)},
		{"bare base64", base64.StdEncoding.EncodeToString(sig)},
		{"bare plus sighash flag", hex.EncodeToString(bareWithFlag)},
		{"witness base64, SIGHASH_DEFAULT", base64.StdEncoding.EncodeToString(witnessDefault)},
		{"witness base64, SIGHASH_ALL (Sparrow)", base64.StdEncoding.EncodeToString(witnessAll)},
		{"witness hex, SIGHASH_ALL", hex.EncodeToString(witnessAll)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := DecodeSignature(tc.text)
			if err != nil {
				t.Fatalf("DecodeSignature: %v", err)
			}
			if string(got[:]) != string(sig) {
				t.Error("decoded signature did not match")
			}
		})
	}
}

// A real signature copied out of Sparrow, to catch a regression that a
// synthetic witness would not: this is the exact string the wallet produced.
//
// Current Sparrow tags the base64 "smp" for the BIP-322 simple variant. The
// untagged form is what older wallets and command line signers emit, and both
// have to decode to the same 64 bytes. The tag is three characters, which is
// not a whole base64 group, so leaving it in place corrupts every byte that
// follows rather than just the first few.
func TestDecodeSignatureAcceptsRealSparrowOutput(t *testing.T) {
	const want = "7726df0193523cf5e071bbc0e43e8c39bcc4c8e583d0f5c02a0c1d29c93bb637370951176f0b8393984febc57bc3ff83b18fceaa752f55dff9152d1a7bb31f7b"

	cases := map[string]string{
		"tagged":   "smpAUF3Jt8Bk1I89eBxu8DkPow5vMTI5YPQ9cAqDB0pyTu2NzcJURdvC4OTmE/rxXvD/4Oxj86qdS9V3/kVLRp7sx97AQ==",
		"untagged": "AUF3Jt8Bk1I89eBxu8DkPow5vMTI5YPQ9cAqDB0pyTu2NzcJURdvC4OTmE/rxXvD/4Oxj86qdS9V3/kVLRp7sx97AQ==",
	}

	for name, sparrow := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := DecodeSignature(sparrow)
			if err != nil {
				t.Fatalf("DecodeSignature: %v", err)
			}
			if hex.EncodeToString(got[:]) != want {
				t.Errorf("got %s, want %s", hex.EncodeToString(got[:]), want)
			}
		})
	}
}

// The other two BIP-322 variants carry a whole transaction or a PSBT, neither
// of which this node can verify. They have to be named as such: without the
// tag they decoded to the wrong length and blamed the paste, which tells the
// user nothing about the Format button that actually produced the problem.
func TestDecodeSignatureNamesUnsupportedVariants(t *testing.T) {
	for _, variant := range []string{VariantFull, VariantProofOfFunds} {
		t.Run(variant, func(t *testing.T) {
			_, err := DecodeSignature(variant + "AUF3Jt8Bk1I89eBxu8DkPow5vMTI5YPQ9cAqDB0pyTu2Nz")
			if err == nil {
				t.Fatalf("accepted a %q signature", variant)
			}
			if !strings.Contains(err.Error(), variant) {
				t.Errorf("error does not name the variant: %v", err)
			}
			if !strings.Contains(err.Error(), VariantSimple) {
				t.Errorf("error does not name the supported variant: %v", err)
			}
		})
	}
}

func TestDecodeSignatureRejectsUnknownSighash(t *testing.T) {
	bad := append(append([]byte{}, testSig()...), 0x83) // SIGHASH_SINGLE|ANYONECANPAY
	if _, err := DecodeSignature(hex.EncodeToString(bad)); err == nil {
		t.Error("expected an error for an unsupported sighash flag")
	}
}

// Sparrow wraps the signature across two lines in its dialog, so a copy from
// that field carries a line break in the middle of the string. Neither hex nor
// base64 decoding tolerates one, and the resulting error blamed the signature
// rather than the paste.
func TestDecodeSignatureToleratesWhitespace(t *testing.T) {
	const sparrow = "AUF3Jt8Bk1I89eBxu8DkPow5vMTI5YPQ9cAqDB0pyTu2NzcJURdvC4OTmE/rxXvD/4Oxj86qdS9V3/kVLRp7sx97AQ=="
	const want = "7726df0193523cf5e071bbc0e43e8c39bcc4c8e583d0f5c02a0c1d29c93bb637370951176f0b8393984febc57bc3ff83b18fceaa752f55dff9152d1a7bb31f7b"

	cases := map[string]string{
		"line break mid-string": sparrow[:49] + "\n" + sparrow[49:],
		"space mid-string":      sparrow[:49] + " " + sparrow[49:],
		"CRLF mid-string":       sparrow[:49] + "\r\n" + sparrow[49:],
		"leading and trailing":  "  \n" + sparrow + "\n  ",
		"tabs throughout":       sparrow[:20] + "\t" + sparrow[20:60] + "\t" + sparrow[60:],
	}
	for name, text := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := DecodeSignature(text)
			if err != nil {
				t.Fatalf("DecodeSignature: %v", err)
			}
			if hex.EncodeToString(got[:]) != want {
				t.Error("decoded signature did not match")
			}
		})
	}
}

// Unpadded and URL-safe base64 turn up when a signature has been through a
// chat window or a URL on its way to the form.
func TestDecodeSignatureAcceptsBase64Variants(t *testing.T) {
	sig := testSig()
	witness := append(append([]byte{0x01, 0x41}, sig...), 0x01)

	for name, text := range map[string]string{
		"padded":       base64.StdEncoding.EncodeToString(witness),
		"unpadded":     base64.RawStdEncoding.EncodeToString(witness),
		"url-safe":     base64.URLEncoding.EncodeToString(witness),
		"url unpadded": base64.RawURLEncoding.EncodeToString(witness),
	} {
		t.Run(name, func(t *testing.T) {
			got, err := DecodeSignature(text)
			if err != nil {
				t.Fatalf("DecodeSignature: %v", err)
			}
			if string(got[:]) != string(sig) {
				t.Error("decoded signature did not match")
			}
		})
	}
}

// The error should point at what is actually wrong with the paste.
func TestDecodeSignatureNamesOffendingCharacter(t *testing.T) {
	_, err := DecodeSignature("AUF3Jt8Bk1I89eBxu8DkPow5vMTI5YPQ9cAqDB0py#u2Nzc")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "'#'") && !strings.Contains(err.Error(), `"#"`) {
		t.Errorf("error should name the offending character, got: %v", err)
	}
}

// Pasting the message into the signature box is an easy mistake when the two
// fields sit next to each other, and "not valid base64" gives the user nothing
// to act on. The error has to name the likely cause.
func TestDecodeSignatureDetectsSwappedFields(t *testing.T) {
	_, err := DecodeSignature("Hello")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "swapped") {
		t.Errorf("error should mention swapped fields, got: %v", err)
	}
}
