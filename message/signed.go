package message

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"
	"unicode"
)

// This file holds the assembly path for messages that were signed somewhere
// else — a hardware wallet, Sparrow, any BIP-322 signer. Nothing here accepts a
// private key or a descriptor: the caller supplies the outpoint it controls,
// the 64-byte signature the wallet produced, and the exact bytes that were
// signed. It is deliberately shared by the CLI and the node's HTTP API so both
// submit byte-identical messages; when they diverged, a message accepted from
// one path was rejected from the other.

// ParseTxID decodes a displayed transaction ID. The wire format stores the
// txid exactly as it is displayed, so the bytes are copied without the
// endian swap that talking to Bitcoin Core requires.
func ParseTxID(s string) ([32]byte, error) {
	var txid [32]byte

	raw, err := hex.DecodeString(strings.TrimPrefix(strings.TrimSpace(s), "0x"))
	if err != nil || len(raw) != 32 {
		return txid, fmt.Errorf("txid must be exactly 64 hexadecimal characters")
	}
	copy(txid[:], raw)

	return txid, nil
}

// SighashAll is the explicit BIP-341 sighash flag Sparrow appends to a
// signature. SIGHASH_DEFAULT is the implicit alternative and appends nothing.
const SighashAll = 0x01

// BIP-322 variant tags. A late revision of the BIP has signers prepend one of
// these three ASCII characters to the base64 so a verifier can tell the three
// serializations apart without trial decoding every one of them. Sparrow tags
// its output; older wallets and command line tooling do not.
const (
	VariantSimple       = "smp" // base64 of a consensus-encoded witness stack
	VariantFull         = "ful" // base64 of the whole to_sign transaction
	VariantProofOfFunds = "pof" // base64 of a finalized PSBT
)

// DecodeSignature extracts the 64-byte Schnorr signature from whatever a
// wallet hands the user.
//
// Sparrow's "BIP322 (Simple)" output is not a bare signature: it is the
// variant tag "smp" followed by the base64 of a serialized witness stack,
// "01 41 <65 bytes>", where the item is the signature followed by an explicit
// SIGHASH_ALL flag. Command line tooling tends to print the bare 64 bytes as
// hex instead. Accepting only one of these rejects a perfectly valid signature
// with an error the user cannot act on, because what they pasted is exactly
// what their wallet gave them.
//
// The trailing sighash flag is dropped because the wire format has room for
// only 64 bytes. It is not lost information: see VerifySignature, which
// retries with the flag re-appended.
func DecodeSignature(text string) ([64]byte, error) {
	var sig [64]byte

	// Strip every space, tab, and line break rather than only trimming the
	// ends. Sparrow wraps the signature across two lines in its dialog, so a
	// plain copy carries a newline in the middle of the string, and neither
	// hex nor base64 decoding tolerates one. That produced an error blaming
	// the signature when the only problem was the paste.
	text = strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, text)
	text = strings.TrimPrefix(text, "0x")

	if text == "" {
		return sig, fmt.Errorf("signature is empty")
	}

	text, err := stripVariantTag(text)
	if err != nil {
		return sig, err
	}

	raw, err := decodeHexOrBase64(text)
	if err != nil {
		return sig, err
	}

	body, err := unwrapWitness(raw)
	if err != nil {
		return sig, err
	}
	copy(sig[:], body)

	return sig, nil
}

// stripVariantTag removes the BIP-322 variant tag a signer may prepend to the
// base64 signature, and rejects the two variants this node cannot verify.
//
// The tag is three literal characters in front of the base64 text rather than
// part of the encoded bytes, and three characters is not a whole number of
// base64 groups. So a tag left in place does not merely add junk at the front:
// it shifts the entire stream by 18 bits and every byte after it decodes to
// noise. That surfaced as "signature is not valid hex or base64", which sends
// the user back to a wallet that did exactly what the BIP tells it to.
//
// An untagged signature is treated as "simple". The BIP asks verifiers to
// assume that variant when no tag is present, because every implementation
// that predates the tag produces it.
func stripVariantTag(text string) (string, error) {
	switch {
	case strings.HasPrefix(text, VariantSimple):
		return strings.TrimPrefix(text, VariantSimple), nil
	case strings.HasPrefix(text, VariantFull):
		return "", fmt.Errorf(
			"signature is a BIP-322 %q signature (a serialized to_sign transaction); "+
				"this node verifies the %q variant only. In Sparrow, set Format to "+
				"\"BIP322 (Simple)\" and sign again", VariantFull, VariantSimple)
	case strings.HasPrefix(text, VariantProofOfFunds):
		return "", fmt.Errorf(
			"signature is a BIP-322 %q proof-of-funds signature (a finalized PSBT); "+
				"this node verifies the %q variant only. In Sparrow, set Format to "+
				"\"BIP322 (Simple)\" and sign again", VariantProofOfFunds, VariantSimple)
	}

	return text, nil
}

// decodeHexOrBase64 tries every encoding a wallet or CLI might produce. Base64
// is attempted both with and without padding, and in the URL-safe alphabet,
// because a signature that survives a trip through a chat window or a URL can
// arrive in any of them.
func decodeHexOrBase64(text string) ([]byte, error) {
	if raw, err := hex.DecodeString(text); err == nil {
		return raw, nil
	}
	for _, enc := range []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	} {
		if raw, err := enc.DecodeString(text); err == nil {
			return raw, nil
		}
	}

	// Name the first offending character. "Neither valid hex nor base64" sends
	// the user back to a wallet that did nothing wrong; pointing at the actual
	// character usually identifies a truncated or decorated paste immediately.
	for i, r := range text {
		if !strings.ContainsRune(
			"ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/-_=", r) {
			return nil, fmt.Errorf(
				"signature contains %q at position %d, which is not valid hex or base64", r, i)
		}
	}

	// A BIP-322 signature is around 90 base64 characters. Something much
	// shorter is almost always the message pasted into the signature box,
	// which is an easy mistake to make and an impossible one to diagnose from
	// "not valid base64".
	if len(text) < 40 {
		return nil, fmt.Errorf(
			"that does not look like a signature (%d characters; a BIP-322 signature is around 90). "+
				"Check the signature and payload fields are not swapped: the signature is the long "+
				"base64 string ending in \"=\"", len(text))
	}

	return nil, fmt.Errorf(
		"signature is not valid hex or base64; it decoded to nothing usable. " +
			"Copy the whole Signature field from your wallet, including any trailing \"=\"")
}

// unwrapWitness reduces any accepted encoding to the bare 64 signature bytes.
func unwrapWitness(raw []byte) ([]byte, error) {
	// A single-item witness stack: item count, then the item length.
	if len(raw) >= 2 && raw[0] == 0x01 && int(raw[1]) == len(raw)-2 {
		raw = raw[2:]
	}

	switch len(raw) {
	case SignatureSize:
		return raw, nil
	case SignatureSize + 1:
		// Signature plus an explicit sighash flag. Only SIGHASH_ALL appears in
		// practice; anything else is likelier a corrupted paste than a real
		// signature, and accepting it would fail later with a worse message.
		if raw[SignatureSize] != SighashAll {
			return nil, fmt.Errorf(
				"signature carries unsupported sighash flag 0x%02x; expected SIGHASH_ALL (0x01)",
				raw[SignatureSize])
		}
		return raw[:SignatureSize], nil
	}

	return nil, fmt.Errorf(
		"signature must contain %d bytes; got %d after decoding. Paste the signature "+
			"exactly as your wallet produced it (Sparrow: BIP322 Simple)",
		SignatureSize, len(raw))
}

// BuildSigned assembles a wire message from a wallet-produced signature. The
// payload must be the exact bytes the wallet signed; any difference, including
// trailing whitespace a form might add, changes the BIP-322 digest and the node
// will reject the signature as invalid.
func BuildSigned(txidText string, vout uint32, signatureText string, payload []byte) ([]byte, error) {
	txid, err := ParseTxID(txidText)
	if err != nil {
		return nil, err
	}

	sig, err := DecodeSignature(signatureText)
	if err != nil {
		return nil, err
	}

	var outpoint Outpoint
	copy(outpoint[:32], txid[:])
	binary.LittleEndian.PutUint32(outpoint[32:36], vout)

	msg, err := NewMessage(outpoint, sig, payload)
	if err != nil {
		return nil, err
	}

	return msg.Serialize(), nil
}
