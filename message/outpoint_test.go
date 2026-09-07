package message

import (
	"encoding/binary"
	"testing"
)

// TestOutpointStringRoundTrips is the regression test for the defect where
// ToString read the vout big-endian while Serialize wrote it little-endian, so
// every outpoint with a non-zero index was displayed and parsed incorrectly.
func TestOutpointStringRoundTrips(t *testing.T) {
	const txid = "674bc8626c339bb6e92f98a8d0354d3aa82ec6df8e50ff60deadb9882df4d3dd"

	for _, vout := range []uint32{0, 1, 2, 255, 256, 70000, 4294967295} {
		op, err := ParseOutpoint(txid + ":" + itoa(vout))
		if err != nil {
			t.Fatalf("ParseOutpoint(vout=%d) returned an error: %v", vout, err)
		}

		// The stored bytes must match what Serialize writes, or the node and
		// the API would disagree about which UTXO a message refers to.
		if got := binary.LittleEndian.Uint32(op[32:36]); got != vout {
			t.Errorf("stored index = %d, want %d", got, vout)
		}

		if got, want := op.ToString(), txid+":"+itoa(vout); got != want {
			t.Errorf("ToString() = %s, want %s", got, want)
		}

		// ToTxidIdx is what the UTXO lookup uses; it must agree with ToString.
		if _, idx := op.ToTxidIdx(); idx != vout {
			t.Errorf("ToTxidIdx() index = %d, want %d", idx, vout)
		}
	}
}

func TestParseOutpointRejectsMalformedInput(t *testing.T) {
	tests := []struct{ name, input string }{
		{"no separator", "674bc862"},
		{"short txid", "abcd:0"},
		{"non-hex txid", "zz4bc8626c339bb6e92f98a8d0354d3aa82ec6df8e50ff60deadb9882df4d3dd:0"},
		{"negative vout", "674bc8626c339bb6e92f98a8d0354d3aa82ec6df8e50ff60deadb9882df4d3dd:-1"},
		{"vout out of range", "674bc8626c339bb6e92f98a8d0354d3aa82ec6df8e50ff60deadb9882df4d3dd:4294967296"},
		{"empty vout", "674bc8626c339bb6e92f98a8d0354d3aa82ec6df8e50ff60deadb9882df4d3dd:"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := ParseOutpoint(test.input); err == nil {
				t.Fatalf("ParseOutpoint(%q) succeeded, want an error", test.input)
			}
		})
	}
}

func itoa(v uint32) string {
	if v == 0 {
		return "0"
	}
	var buf []byte
	for v > 0 {
		buf = append([]byte{byte('0' + v%10)}, buf...)
		v /= 10
	}
	return string(buf)
}
