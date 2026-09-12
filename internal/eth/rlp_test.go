package eth

import (
	"bytes"
	"encoding/hex"
	"strings"
	"testing"
)

func TestRlpEncodeBytesLongString(t *testing.T) {
	tests := []struct {
		name       string
		length     int
		wantPrefix string
	}{
		{"55 bytes is still short form", 55, "b7"},
		{"56 bytes switches to long form", 56, "b838"},
		{"255 bytes uses one length byte", 255, "b8ff"},
		{"256 bytes uses two length bytes", 256, "b90100"},
		{"1024 bytes", 1024, "b90400"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded := hex.EncodeToString(rlpEncodeBytes(bytes.Repeat([]byte{0xab}, tt.length)))
			if !strings.HasPrefix(encoded, tt.wantPrefix) {
				t.Errorf("prefix: got %s..., want %s...", encoded[:6], tt.wantPrefix)
			}
			wantLen := len(tt.wantPrefix)/2 + tt.length
			if got := len(encoded) / 2; got != wantLen {
				t.Errorf("encoded length: got %d, want %d", got, wantLen)
			}
		})
	}
}

func TestRlpEncodeListFromRLPLongPayload(t *testing.T) {
	// One 60-byte string: 2 header bytes + 60 = 62-byte payload -> long list form.
	item := rlpEncodeBytes(bytes.Repeat([]byte{0x01}, 60))
	encoded := rlpEncodeListFromRLP([][]byte{item})
	if encoded[0] != 0xf8 {
		t.Errorf("prefix: got 0x%02x, want 0xf8", encoded[0])
	}
	if int(encoded[1]) != len(item) {
		t.Errorf("declared payload length: got %d, want %d", encoded[1], len(item))
	}
}

func TestRlpEncodeListFromRLPEmpty(t *testing.T) {
	if got := hex.EncodeToString(rlpEncodeListFromRLP(nil)); got != "c0" {
		t.Errorf("got %s, want c0", got)
	}
}

func TestEncodeLength(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{0, "00"},
		{1, "01"},
		{255, "ff"},
		{256, "0100"},
		{65536, "010000"},
	}
	for _, tt := range tests {
		if got := hex.EncodeToString(encodeLength(tt.n)); got != tt.want {
			t.Errorf("encodeLength(%d) = %s, want %s", tt.n, got, tt.want)
		}
	}
}

func TestHexToUint64EdgeCases(t *testing.T) {
	tests := []struct {
		input string
		want  uint64
	}{
		{"", 0},
		{"0x", 0},
		{"0xABCDEF", 0xabcdef},
		{"deadbeef", 0xdeadbeef},
		{"0xffffffffffffffff", 1<<64 - 1},
	}
	for _, tt := range tests {
		if got := hexToUint64(tt.input); got != tt.want {
			t.Errorf("hexToUint64(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestHexToBytesOddLength(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"0x", ""},
		{"0x1", "01"},
		{"0xabc", "0abc"},
	}
	for _, tt := range tests {
		if got := hex.EncodeToString(hexToBytes(tt.input)); got != tt.want {
			t.Errorf("hexToBytes(%q) = %s, want %s", tt.input, got, tt.want)
		}
	}
}

func TestDecodeUintBE(t *testing.T) {
	tests := []struct {
		input []byte
		want  int
	}{
		{nil, 0},
		{[]byte{0x00}, 0},
		{[]byte{0x01}, 1},
		{[]byte{0xff}, 255},
		{[]byte{0x01, 0x00}, 256},
		{[]byte{0x01, 0x02, 0x03}, 0x010203},
	}
	for _, tt := range tests {
		if got := decodeUintBE(tt.input); got != tt.want {
			t.Errorf("decodeUintBE(%x) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestCompactToNibblesRoundTrip(t *testing.T) {
	tests := []struct {
		name    string
		nibbles []byte
		isLeaf  bool
	}{
		{"empty leaf", nil, true},
		{"empty extension", nil, false},
		{"even leaf", []byte{1, 2, 3, 4}, true},
		{"odd leaf", []byte{1, 2, 3}, true},
		{"even extension", []byte{6, 4, 6, 15}, false},
		{"odd extension", []byte{6}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compact := nibblesToCompact(tt.nibbles, tt.isLeaf)
			decoded := compactToNibbles(compact)
			if len(decoded) == 0 {
				t.Fatal("compactToNibbles returned nothing")
			}
			// The first nibble is the hex-prefix flag; the rest is the key.
			gotLeaf := decoded[0]&0x02 != 0
			if gotLeaf != tt.isLeaf {
				t.Errorf("leaf flag: got %v, want %v", gotLeaf, tt.isLeaf)
			}
			if got := hex.EncodeToString(decoded[1:]); got != hex.EncodeToString(tt.nibbles) {
				t.Errorf("nibbles: got %s, want %s", got, hex.EncodeToString(tt.nibbles))
			}
		})
	}
}

func TestCompactToNibblesEmptyInput(t *testing.T) {
	if got := compactToNibbles(nil); got != nil {
		t.Errorf("compactToNibbles(nil) = %v, want nil", got)
	}
}

func TestRlpDecodeItemRaw(t *testing.T) {
	longString := bytes.Repeat([]byte{0x07}, 100)
	longList := rlpEncodeListFromRLP([][]byte{rlpEncodeBytes(longString)})

	tests := []struct {
		name         string
		data         []byte
		wantRaw      string
		wantConsumed int
	}{
		{"single byte", []byte{0x42}, "42", 1},
		{"empty string", []byte{0x80}, "", 1},
		{"short string", rlpEncodeBytes([]byte("dog")), "646f67", 4},
		{"long string", rlpEncodeBytes(longString), hex.EncodeToString(longString), 102},
		// Lists keep their header so the result stays a self-describing node.
		{"short list", []byte{0xc2, 0x33, 0x32}, "c23332", 3},
		{"long list", longList, hex.EncodeToString(longList), len(longList)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, consumed := rlpDecodeItemRaw(tt.data)
			if hex.EncodeToString(raw) != tt.wantRaw {
				t.Errorf("raw: got %x, want %s", raw, tt.wantRaw)
			}
			if consumed != tt.wantConsumed {
				t.Errorf("consumed: got %d, want %d", consumed, tt.wantConsumed)
			}
		})
	}
}

func TestRlpDecodeItemRawTruncated(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"empty input", nil},
		{"short string cut off", []byte{0x83, 0x01}},
		{"long string cut off", []byte{0xb8, 0x40, 0x01}},
		{"short list cut off", []byte{0xc4, 0x01}},
		{"long list cut off", []byte{0xf8, 0x40, 0x01}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, _ := rlpDecodeItemRaw(tt.data)
			if raw != nil {
				t.Errorf("expected nil for truncated input, got %x", raw)
			}
		})
	}
}

func TestRlpDecodeListRaw(t *testing.T) {
	list := rlpEncodeListFromRLP([][]byte{
		rlpEncodeBytes([]byte("cat")),
		rlpEncodeBytes([]byte("dog")),
		{0xc2, 0x33, 0x32},
	})
	items := rlpDecodeListRaw(list)
	if len(items) != 3 {
		t.Fatalf("items: got %d, want 3", len(items))
	}
	if string(items[0]) != "cat" || string(items[1]) != "dog" {
		t.Errorf("string items: got %q, %q", items[0], items[1])
	}
	if hex.EncodeToString(items[2]) != "c23332" {
		t.Errorf("nested list: got %x, want c23332", items[2])
	}
}

func TestRlpDecodeListRawNotAList(t *testing.T) {
	for _, data := range [][]byte{nil, {}, rlpEncodeBytes([]byte("dog"))} {
		if items := rlpDecodeListRaw(data); items != nil {
			t.Errorf("rlpDecodeListRaw(%x) = %v, want nil", data, items)
		}
	}
}

func TestRlpDecodeListPayload(t *testing.T) {
	longPayload := bytes.Repeat([]byte{0x01}, 300)
	longList := append([]byte{0xf9, 0x01, 0x2c}, longPayload...)

	tests := []struct {
		name          string
		data          []byte
		wantHeaderLen int
		wantPayload   int
	}{
		{"empty input", nil, 0, -1},
		{"not a list", []byte{0x83, 0x61, 0x62, 0x63}, 0, -1},
		{"empty list", []byte{0xc0}, 1, 0},
		{"short list", []byte{0xc2, 0x33, 0x32}, 1, 2},
		{"long list", longList, 3, 300},
		{"short list truncated", []byte{0xc4, 0x01}, 0, -1},
		{"long list header truncated", []byte{0xf9, 0x01}, 0, -1},
		{"long list payload truncated", []byte{0xf9, 0x01, 0x2c, 0x01}, 0, -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			headerLen, payload := rlpDecodeListPayload(tt.data)
			if headerLen != tt.wantHeaderLen {
				t.Errorf("headerLen: got %d, want %d", headerLen, tt.wantHeaderLen)
			}
			if tt.wantPayload < 0 {
				if payload != nil {
					t.Errorf("payload: got %x, want nil", payload)
				}
				return
			}
			if len(payload) != tt.wantPayload {
				t.Errorf("payload length: got %d, want %d", len(payload), tt.wantPayload)
			}
		})
	}
}
