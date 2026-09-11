package eth

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// The reference roots in testdata/ were produced with go-ethereum's own trie
// implementation (trie.Trie.Hash for mpt_roots.json, types.DeriveSha for
// receipt_roots.json). go-ethereum is deliberately not a dependency of this
// module, so the expected values are checked in as static fixtures instead.

type mptVector struct {
	KV   [][2]string `json:"kv"` // hex-encoded key/value pairs, in insertion order
	Root string      `json:"root"`
}

type receiptVector struct {
	Name     string           `json:"name"`
	Receipts []map[string]any `json:"receipts"`
	Root     string           `json:"root"`
}

func loadMPTVectors(t *testing.T) []mptVector {
	t.Helper()
	data, err := os.ReadFile("testdata/mpt_roots.json")
	if err != nil {
		t.Fatalf("read vectors: %v", err)
	}
	var vectors []mptVector
	if err := json.Unmarshal(data, &vectors); err != nil {
		t.Fatalf("decode vectors: %v", err)
	}
	if len(vectors) == 0 {
		t.Fatal("no vectors found")
	}
	return vectors
}

func decodeKV(t *testing.T, pair [2]string) (key, value []byte) {
	t.Helper()
	key, err := hex.DecodeString(pair[0])
	if err != nil {
		t.Fatalf("decode key %q: %v", pair[0], err)
	}
	value, err = hex.DecodeString(pair[1])
	if err != nil {
		t.Fatalf("decode value %q: %v", pair[1], err)
	}
	return key, value
}

func trieRoot(t *testing.T, kv [][2]string) string {
	t.Helper()
	trie := newMPT()
	for _, pair := range kv {
		key, value := decodeKV(t, pair)
		trie.put(key, value)
	}
	return "0x" + hex.EncodeToString(trie.rootHash())
}

func TestMPTRootMatchesReferenceVectors(t *testing.T) {
	for i, v := range loadMPTVectors(t) {
		if got := trieRoot(t, v.KV); got != v.Root {
			t.Errorf("vector %d (%d keys): got %s, want %s", i, len(v.KV), got, v.Root)
		}
	}
}

// TestMPTRootIndependentOfInsertionOrder pins the invariant that broke when
// decodeNode stripped the RLP header off inline child nodes: a trie root must
// depend only on the key/value set, never on the order the pairs were inserted.
func TestMPTRootIndependentOfInsertionOrder(t *testing.T) {
	for i, v := range loadMPTVectors(t) {
		if len(v.KV) < 2 || len(v.KV) > 32 {
			continue
		}
		reversed := make([][2]string, len(v.KV))
		for j, pair := range v.KV {
			reversed[len(v.KV)-1-j] = pair
		}
		if got := trieRoot(t, reversed); got != v.Root {
			t.Errorf("vector %d reversed (%d keys): got %s, want %s", i, len(v.KV), got, v.Root)
		}

		rotated := append(append([][2]string{}, v.KV[len(v.KV)/2:]...), v.KV[:len(v.KV)/2]...)
		if got := trieRoot(t, rotated); got != v.Root {
			t.Errorf("vector %d rotated (%d keys): got %s, want %s", i, len(v.KV), got, v.Root)
		}
	}
}

// TestMPTExtensionWithInlineChild is the minimal regression case for the
// inline-child bug: {"ab","abc","b"} builds an extension node whose child
// branch is short enough (<32 bytes) to be embedded rather than hashed.
func TestMPTExtensionWithInlineChild(t *testing.T) {
	trie := newMPT()
	trie.put([]byte("ab"), []byte("1"))
	trie.put([]byte("abc"), []byte("2"))
	trie.put([]byte("b"), []byte("3"))

	const want = "0x123b9cef138dbab9167c3ef8c60d681bea2379791f31bff07c880873e94aafb0"
	if got := "0x" + hex.EncodeToString(trie.rootHash()); got != want {
		t.Errorf("root: got %s, want %s", got, want)
	}
}

func TestMPTEmptyRoot(t *testing.T) {
	trie := newMPT()
	const want = "56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421"
	if got := hex.EncodeToString(trie.rootHash()); got != want {
		t.Errorf("empty root: got %s, want %s", got, want)
	}
}

func TestMPTOverwriteExistingKey(t *testing.T) {
	overwritten := newMPT()
	overwritten.put([]byte("dog"), []byte("puppy"))
	overwritten.put([]byte("horse"), []byte("mare"))
	overwritten.put([]byte("horse"), []byte("stallion"))

	direct := newMPT()
	direct.put([]byte("dog"), []byte("puppy"))
	direct.put([]byte("horse"), []byte("stallion"))

	got := hex.EncodeToString(overwritten.rootHash())
	want := hex.EncodeToString(direct.rootHash())
	if got != want {
		t.Errorf("overwriting a key changed the root: got %s, want %s", got, want)
	}
}

// TestMPTShortValueStaysInline covers rootHash's "root node smaller than 32
// bytes" branch, where the root is the inline encoding rather than a hash.
func TestMPTShortValueStaysInline(t *testing.T) {
	trie := newMPT()
	trie.put([]byte{0x01}, []byte("v"))
	if len(trie.root) >= 32 {
		t.Fatalf("expected an inline root node, got %d bytes", len(trie.root))
	}
	if got := trie.rootHash(); len(got) != 32 {
		t.Errorf("rootHash must always be 32 bytes, got %d", len(got))
	}
}

func TestResolveNode(t *testing.T) {
	trie := newMPT()
	stored := make([]byte, 40)
	hash := trie.hashNode(stored)
	if len(hash) != 32 {
		t.Fatalf("hashNode should hash nodes >= 32 bytes, got %d bytes", len(hash))
	}
	if got := trie.resolveNode(hash); string(got) != string(stored) {
		t.Errorf("resolveNode(hash) did not return the stored node")
	}

	inline := []byte{0xc2, 0x33, 0x32}
	if got := trie.resolveNode(inline); string(got) != string(inline) {
		t.Errorf("resolveNode(inline) = %x, want %x", got, inline)
	}

	unknown := make([]byte, 32)
	if got := trie.resolveNode(unknown); got != nil {
		t.Errorf("resolveNode(unknown hash) = %x, want nil", got)
	}
}

func TestComputeReceiptsRootMatchesReferenceVectors(t *testing.T) {
	data, err := os.ReadFile("testdata/receipt_roots.json")
	if err != nil {
		t.Fatalf("read vectors: %v", err)
	}
	var vectors []receiptVector
	if err := json.Unmarshal(data, &vectors); err != nil {
		t.Fatalf("decode vectors: %v", err)
	}
	if len(vectors) == 0 {
		t.Fatal("no vectors found")
	}

	for _, v := range vectors {
		t.Run(v.Name, func(t *testing.T) {
			got, err := ComputeReceiptsRoot(v.Receipts)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != v.Root {
				t.Errorf("got %s, want %s", got, v.Root)
			}
		})
	}
}

func TestComputeReceiptsRootMissingStatusAndRoot(t *testing.T) {
	_, err := ComputeReceiptsRoot([]map[string]any{{
		"cumulativeGasUsed": "0x5208",
		"logsBloom":         "0x00",
		"logs":              []any{},
		"type":              "0x0",
	}})
	if err == nil {
		t.Fatal("expected an error for a receipt with neither status nor root")
	}
}

// TestComputeReceiptsRootIgnoresMalformedLogs documents that non-object entries
// in the logs array are skipped rather than aborting the computation.
func TestComputeReceiptsRootIgnoresMalformedLogs(t *testing.T) {
	withGarbage := []map[string]any{{
		"status":            "0x1",
		"cumulativeGasUsed": "0x5208",
		"logsBloom":         "0x00",
		"logs":              []any{"not-a-log", 42},
		"type":              "0x0",
	}}
	withoutLogs := []map[string]any{{
		"status":            "0x1",
		"cumulativeGasUsed": "0x5208",
		"logsBloom":         "0x00",
		"logs":              []any{},
		"type":              "0x0",
	}}

	got, err := ComputeReceiptsRoot(withGarbage)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want, err := ComputeReceiptsRoot(withoutLogs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("malformed logs changed the root: got %s, want %s", got, want)
	}
}

// TestEncodeReceiptTypePrefix checks that non-legacy receipts are prefixed with
// their type byte and legacy ones are not.
func TestEncodeReceiptTypePrefix(t *testing.T) {
	base := func(txType string) map[string]any {
		return map[string]any{
			"status":            "0x1",
			"cumulativeGasUsed": "0x5208",
			"logsBloom":         "0x00",
			"logs":              []any{},
			"type":              txType,
		}
	}

	legacy, err := encodeReceipt(base("0x0"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if legacy[0] < 0xc0 {
		t.Errorf("legacy receipt should start with an RLP list header, got 0x%02x", legacy[0])
	}

	for _, txType := range []struct {
		hex  string
		want byte
	}{{"0x1", 1}, {"0x2", 2}, {"0x3", 3}} {
		encoded, err := encodeReceipt(base(txType.hex))
		if err != nil {
			t.Fatalf("type %s: unexpected error: %v", txType.hex, err)
		}
		if encoded[0] != txType.want {
			t.Errorf("type %s: prefix byte = 0x%02x, want 0x%02x", txType.hex, encoded[0], txType.want)
		}
		if string(encoded[1:]) != string(legacy) {
			t.Errorf("type %s: payload differs from the legacy encoding", txType.hex)
		}
	}
}

func TestEncodeLogEmptyTopics(t *testing.T) {
	encoded, err := encodeLog(map[string]any{
		"address": "0xdac17f958d2ee523a2206206994597c13d831ec7",
		"topics":  []any{},
		"data":    "0x",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// list(address(21 bytes RLP), empty list(1), empty string(1)) = 23 bytes payload.
	want := "d794dac17f958d2ee523a2206206994597c13d831ec7c080"
	if got := hex.EncodeToString(encoded); got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestEncodeBranchEmpty(t *testing.T) {
	children := make([][]byte, 17)
	encoded := encodeBranch(children)
	// 17 empty slots, each RLP 0x80: a 17-byte list payload.
	want := "d1" + strings.Repeat("80", 17)
	if got := hex.EncodeToString(encoded); got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestEncodeBranchHashedAndInlineChildren(t *testing.T) {
	children := make([][]byte, 17)
	hashChild := make([]byte, 32)
	hashChild[0] = 0xaa
	children[0] = hashChild
	children[1] = []byte{0xc2, 0x33, 0x32} // short inline node
	children[16] = []byte("v")

	encoded := encodeBranch(children)
	items := rlpDecodeListRaw(encoded)
	if len(items) != 17 {
		t.Fatalf("expected 17 items, got %d", len(items))
	}
	if hex.EncodeToString(items[0]) != hex.EncodeToString(hashChild) {
		t.Errorf("32-byte child should round-trip as a hash reference")
	}
	if hex.EncodeToString(items[1]) != "c23332" {
		t.Errorf("inline child should be embedded verbatim, got %x", items[1])
	}
	if string(items[16]) != "v" {
		t.Errorf("value slot: got %q, want %q", items[16], "v")
	}
}

func TestEncodeExtension(t *testing.T) {
	hashChild := make([]byte, 32)
	hashChild[31] = 0x01

	encoded := encodeExtension([]byte{6, 2}, hashChild)
	nodeType, decoded := decodeNode(encoded)
	if nodeType != nodeTypeExtension {
		t.Fatalf("nodeType = %d, want extension", nodeType)
	}
	if hex.EncodeToString(decoded[0]) != "0602" {
		t.Errorf("nibbles: got %x, want 0602", decoded[0])
	}
	if hex.EncodeToString(decoded[1]) != hex.EncodeToString(hashChild) {
		t.Errorf("child ref: got %x, want %x", decoded[1], hashChild)
	}
}

// TestDecodeNodePreservesInlineChild is the unit-level counterpart of the
// inline-child regression: an extension whose child is embedded must decode
// back to a complete, self-describing RLP node.
func TestDecodeNodePreservesInlineChild(t *testing.T) {
	inlineChild := []byte{0xc2, 0x33, 0x32}
	encoded := encodeExtension([]byte{6, 2}, inlineChild)

	nodeType, decoded := decodeNode(encoded)
	if nodeType != nodeTypeExtension {
		t.Fatalf("nodeType = %d, want extension", nodeType)
	}
	if hex.EncodeToString(decoded[1]) != hex.EncodeToString(inlineChild) {
		t.Errorf("inline child lost its RLP header: got %x, want %x", decoded[1], inlineChild)
	}
}

func TestDecodeNodeLeaf(t *testing.T) {
	encoded := encodeLeaf([]byte{1, 2, 3}, []byte("value"))
	nodeType, decoded := decodeNode(encoded)
	if nodeType != nodeTypeLeaf {
		t.Fatalf("nodeType = %d, want leaf", nodeType)
	}
	if hex.EncodeToString(decoded[0]) != "010203" {
		t.Errorf("nibbles: got %x, want 010203", decoded[0])
	}
	if string(decoded[1]) != "value" {
		t.Errorf("value: got %q, want %q", decoded[1], "value")
	}
}

func TestDecodeNodeBranch(t *testing.T) {
	children := make([][]byte, 17)
	children[3] = []byte{0xc2, 0x33, 0x32}
	nodeType, items := decodeNode(encodeBranch(children))
	if nodeType != nodeTypeBranch {
		t.Fatalf("nodeType = %d, want branch", nodeType)
	}
	if len(items) != 17 {
		t.Errorf("branch items: got %d, want 17", len(items))
	}
}

func TestDecodeNodeUnrecognised(t *testing.T) {
	// A three-item list is neither a leaf/extension pair nor a branch.
	three := rlpEncodeListFromRLP([][]byte{{0x01}, {0x02}, {0x03}})
	if nodeType, items := decodeNode(three); nodeType != -1 || items != nil {
		t.Errorf("decodeNode(3-item list) = (%d, %v), want (-1, nil)", nodeType, items)
	}
}

func TestDecodeBranchRefsNormalisesEmptySlots(t *testing.T) {
	children := make([][]byte, 17)
	children[5] = []byte{0xc2, 0x33, 0x32}
	encoded := encodeBranch(children)

	refs := decodeBranchRefs(nil, encoded)
	if len(refs) != 17 {
		t.Fatalf("refs: got %d, want 17", len(refs))
	}
	for i, ref := range refs {
		if i == 5 {
			if hex.EncodeToString(ref) != "c23332" {
				t.Errorf("refs[5] = %x, want c23332", ref)
			}
			continue
		}
		if ref != nil {
			t.Errorf("refs[%d] = %x, want nil", i, ref)
		}
	}
}

func TestCommonPrefixLen(t *testing.T) {
	tests := []struct {
		a, b []byte
		want int
	}{
		{nil, nil, 0},
		{[]byte{1, 2, 3}, nil, 0},
		{[]byte{1, 2, 3}, []byte{1, 2, 3}, 3},
		{[]byte{1, 2, 3}, []byte{1, 2}, 2},
		{[]byte{1, 2}, []byte{1, 2, 3}, 2},
		{[]byte{9, 2, 3}, []byte{1, 2, 3}, 0},
	}
	for _, tt := range tests {
		if got := commonPrefixLen(tt.a, tt.b); got != tt.want {
			t.Errorf("commonPrefixLen(%v, %v) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}
