package eth

import (
	"encoding/hex"
	"testing"
)

func TestKeccak256(t *testing.T) {
	// Keccak256 of empty string
	result := keccak256([]byte{})
	expected := "c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470"
	got := hex.EncodeToString(result)
	if got != expected {
		t.Errorf("keccak256 empty: got %s, want %s", got, expected)
	}
}

func TestRlpEncodeUint(t *testing.T) {
	tests := []struct {
		val  uint64
		want string
	}{
		{0, "80"},
		{1, "01"},
		{127, "7f"},
		{128, "8180"},
		{256, "820100"},
		{1024, "820400"},
	}
	for _, tt := range tests {
		got := hex.EncodeToString(rlpEncodeUint(tt.val))
		if got != tt.want {
			t.Errorf("rlpEncodeUint(%d): got %s, want %s", tt.val, got, tt.want)
		}
	}
}

func TestRlpEncodeBytes(t *testing.T) {
	tests := []struct {
		name string
		val  []byte
		want string
	}{
		{"empty", []byte{}, "80"},
		{"single byte < 128", []byte{0x42}, "42"},
		{"single byte 128", []byte{0x80}, "8180"},
		{"short string", []byte("dog"), "83646f67"},
	}
	for _, tt := range tests {
		got := hex.EncodeToString(rlpEncodeBytes(tt.val))
		if got != tt.want {
			t.Errorf("rlpEncodeBytes(%s): got %s, want %s", tt.name, got, tt.want)
		}
	}
}

func TestRlpEncodeListFromRLP(t *testing.T) {
	// RLP of ["cat", "dog"]
	cat := rlpEncodeBytes([]byte("cat"))
	dog := rlpEncodeBytes([]byte("dog"))
	got := hex.EncodeToString(rlpEncodeListFromRLP([][]byte{cat, dog}))
	want := "c88363617483646f67"
	if got != want {
		t.Errorf("rlpEncodeListFromRLP: got %s, want %s", got, want)
	}
}

func TestHexToBytes(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"0x1234", "1234"},
		{"0xabcd", "abcd"},
		{"1234", "1234"},
		{"0x0", "00"},
	}
	for _, tt := range tests {
		got := hex.EncodeToString(hexToBytes(tt.input))
		if got != tt.want {
			t.Errorf("hexToBytes(%s): got %s, want %s", tt.input, got, tt.want)
		}
	}
}

func TestHexToUint64(t *testing.T) {
	tests := []struct {
		input string
		want  uint64
	}{
		{"0x0", 0},
		{"0x1", 1},
		{"0xa", 10},
		{"0xff", 255},
		{"0x100", 256},
	}
	for _, tt := range tests {
		got := hexToUint64(tt.input)
		if got != tt.want {
			t.Errorf("hexToUint64(%s): got %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestComputeReceiptsRootEmpty(t *testing.T) {
	root, err := ComputeReceiptsRoot(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421"
	if root != want {
		t.Errorf("empty receipts root: got %s, want %s", root, want)
	}
}

func TestBytesToNibbles(t *testing.T) {
	got := bytesToNibbles([]byte{0xab, 0xcd})
	want := []byte{0xa, 0xb, 0xc, 0xd}
	if len(got) != len(want) {
		t.Fatalf("bytesToNibbles length: got %d, want %d", len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("bytesToNibbles[%d]: got %d, want %d", i, got[i], want[i])
		}
	}
}

func TestNibblesToCompactLeaf(t *testing.T) {
	// Leaf with even nibbles [1, 2, 3, 4]
	compact := nibblesToCompact([]byte{1, 2, 3, 4}, true)
	got := hex.EncodeToString(compact)
	want := "2012" + "34"
	if got != want {
		t.Errorf("nibblesToCompact(even leaf): got %s, want %s", got, want)
	}

	// Leaf with odd nibbles [1, 2, 3]
	compact = nibblesToCompact([]byte{1, 2, 3}, true)
	got = hex.EncodeToString(compact)
	want = "31" + "23"
	if got != want {
		t.Errorf("nibblesToCompact(odd leaf): got %s, want %s", got, want)
	}
}

func TestComputeReceiptsRootSingleLegacyReceipt(t *testing.T) {
	// Single legacy receipt (type 0) with status 1, minimal data
	receipt := map[string]any{
		"status":            "0x1",
		"cumulativeGasUsed": "0x5208",
		"logsBloom":         "0x" + "00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
		"logs":              []any{},
		"type":              "0x0",
	}
	root, err := ComputeReceiptsRoot([]map[string]any{receipt})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The root should be a valid hex hash
	if len(root) != 66 { // "0x" + 64 hex chars
		t.Errorf("unexpected root length: %d", len(root))
	}
	// Verify it's not the empty root
	emptyRoot := "0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421"
	if root == emptyRoot {
		t.Error("single receipt should not produce empty root")
	}
}

func TestEncodeReceipt(t *testing.T) {
	receipt := map[string]any{
		"status":            "0x1",
		"cumulativeGasUsed": "0x5208",
		"logsBloom":         "0x" + "00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
		"logs":              []any{},
		"type":              "0x0",
	}
	encoded, err := encodeReceipt(receipt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(encoded) == 0 {
		t.Error("encoded receipt should not be empty")
	}
}

func TestEncodeLog(t *testing.T) {
	logMap := map[string]any{
		"address": "0xdac17f958d2ee523a2206206994597c13d831ec7",
		"topics":  []any{"0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"},
		"data":    "0x0000000000000000000000000000000000000000000000000000000005f5e100",
	}
	encoded, err := encodeLog(logMap)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(encoded) == 0 {
		t.Error("encoded log should not be empty")
	}
}
