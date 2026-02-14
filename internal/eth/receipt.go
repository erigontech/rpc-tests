package eth

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	"golang.org/x/crypto/sha3"
)

// ComputeReceiptsRoot computes the MPT root hash from a list of receipt maps
// returned by eth_getBlockReceipts.
func ComputeReceiptsRoot(receipts []map[string]any) (string, error) {
	if len(receipts) == 0 {
		// Empty trie root = Keccak256(RLP(""))
		return "0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421", nil
	}

	trie := newMPT()

	for i, receipt := range receipts {
		encoded, err := encodeReceipt(receipt)
		if err != nil {
			return "", fmt.Errorf("encode receipt %d: %w", i, err)
		}

		key := rlpEncodeUint(uint64(i))
		trie.put(key, encoded)
	}

	root := trie.rootHash()
	return "0x" + hex.EncodeToString(root), nil
}

func encodeReceipt(receipt map[string]any) ([]byte, error) {
	// Extract fields
	statusVal := receipt["status"]
	cumulativeGasUsed := receipt["cumulativeGasUsed"]
	logsBloom := receipt["logsBloom"]
	logs := receipt["logs"]
	receiptType := receipt["type"]

	// Build logs list
	logsArr, _ := logs.([]any)
	var encodedLogs [][]byte
	for _, l := range logsArr {
		logMap, ok := l.(map[string]any)
		if !ok {
			continue
		}
		encodedLog, err := encodeLog(logMap)
		if err != nil {
			return nil, err
		}
		encodedLogs = append(encodedLogs, encodedLog)
	}

	// Build receipt RLP list
	var items [][]byte

	// Status or root
	if statusVal != nil {
		statusHex, _ := statusVal.(string)
		statusInt := hexToUint64(statusHex)
		items = append(items, rlpEncodeUint(statusInt))
	} else if root, ok := receipt["root"].(string); ok {
		rootBytes := hexToBytes(root)
		items = append(items, rlpEncodeBytes(rootBytes))
	} else {
		return nil, fmt.Errorf("receipt has neither 'status' nor 'root' field")
	}

	// cumulativeGasUsed
	gasHex, _ := cumulativeGasUsed.(string)
	gasVal := hexToUint64(gasHex)
	items = append(items, rlpEncodeUint(gasVal))

	// logsBloom
	bloomHex, _ := logsBloom.(string)
	bloomBytes := hexToBytes(bloomHex)
	items = append(items, rlpEncodeBytes(bloomBytes))

	// logs (each encodedLog is already a full RLP-encoded list)
	items = append(items, rlpEncodeListFromRLP(encodedLogs))

	value := rlpEncodeListFromRLP(items)

	// Receipt type: non-legacy types are prefixed with the type byte
	typeHex, _ := receiptType.(string)
	typeVal := hexToUint64(typeHex)
	if typeVal != 0 {
		value = append([]byte{byte(typeVal)}, value...)
	}

	return value, nil
}

func encodeLog(logMap map[string]any) ([]byte, error) {
	address, _ := logMap["address"].(string)
	topicsRaw, _ := logMap["topics"].([]any)
	data, _ := logMap["data"].(string)

	var items [][]byte

	// address
	items = append(items, rlpEncodeBytes(hexToBytes(address)))

	// topics
	var topicItems [][]byte
	for _, t := range topicsRaw {
		topicStr, _ := t.(string)
		topicItems = append(topicItems, rlpEncodeBytes(hexToBytes(topicStr)))
	}
	items = append(items, rlpEncodeListFromRLP(topicItems))

	// data
	items = append(items, rlpEncodeBytes(hexToBytes(data)))

	return rlpEncodeListFromRLP(items), nil
}

// --- RLP encoding ---

func rlpEncodeUint(val uint64) []byte {
	if val == 0 {
		return []byte{0x80}
	}
	if val < 128 {
		return []byte{byte(val)}
	}
	b := big.NewInt(0).SetUint64(val).Bytes()
	return rlpEncodeBytes(b)
}

func rlpEncodeBytes(b []byte) []byte {
	if len(b) == 1 && b[0] < 128 {
		return b
	}
	if len(b) <= 55 {
		return append([]byte{byte(0x80 + len(b))}, b...)
	}
	lenBytes := encodeLength(len(b))
	prefix := append([]byte{byte(0xb7 + len(lenBytes))}, lenBytes...)
	return append(prefix, b...)
}

func rlpEncodeListFromRLP(rlpItems [][]byte) []byte {
	var payload []byte
	for _, item := range rlpItems {
		payload = append(payload, item...)
	}
	if len(payload) <= 55 {
		return append([]byte{byte(0xc0 + len(payload))}, payload...)
	}
	lenBytes := encodeLength(len(payload))
	prefix := append([]byte{byte(0xf7 + len(lenBytes))}, lenBytes...)
	return append(prefix, payload...)
}

func encodeLength(n int) []byte {
	if n == 0 {
		return []byte{0}
	}
	b := big.NewInt(int64(n)).Bytes()
	return b
}

// --- Hex utilities ---

func hexToBytes(s string) []byte {
	s = strings.TrimPrefix(s, "0x")
	if len(s)%2 != 0 {
		s = "0" + s
	}
	b, _ := hex.DecodeString(s)
	return b
}

func hexToUint64(s string) uint64 {
	s = strings.TrimPrefix(s, "0x")
	if s == "" {
		return 0
	}
	var result uint64
	for _, c := range s {
		result <<= 4
		switch {
		case c >= '0' && c <= '9':
			result |= uint64(c - '0')
		case c >= 'a' && c <= 'f':
			result |= uint64(c - 'a' + 10)
		case c >= 'A' && c <= 'F':
			result |= uint64(c - 'A' + 10)
		}
	}
	return result
}

// --- MPT (Modified Merkle-Patricia Trie) ---

func keccak256(data []byte) []byte {
	h := sha3.NewLegacyKeccak256()
	h.Write(data)
	return h.Sum(nil)
}

// mpt is a simple implementation of Ethereum's Modified Merkle-Patricia Trie
// sufficient for computing root hashes of receipt tries.
type mpt struct {
	db map[string][]byte
	root []byte
}

func newMPT() *mpt {
	return &mpt{
		db: make(map[string][]byte),
	}
}

func (t *mpt) put(key, value []byte) {
	nibbles := bytesToNibbles(key)
	t.root = t.insert(t.root, nibbles, value)
}

func (t *mpt) rootHash() []byte {
	if t.root == nil {
		return keccak256([]byte{0x80})
	}
	if len(t.root) < 32 {
		return keccak256(t.root)
	}
	return t.root
}

func (t *mpt) insert(node []byte, nibbles []byte, value []byte) []byte {
	if node == nil {
		// Create a leaf node
		return t.hashNode(encodeLeaf(nibbles, value))
	}

	// Decode the existing node
	existing := t.resolveNode(node)
	if existing == nil {
		return t.hashNode(encodeLeaf(nibbles, value))
	}

	nodeType, decoded := decodeNode(existing)

	switch nodeType {
	case nodeTypeLeaf:
		existingNibbles := decoded[0]
		existingValue := decoded[1]

		// Find common prefix
		commonLen := commonPrefixLen(nibbles, existingNibbles)

		if commonLen == len(nibbles) && commonLen == len(existingNibbles) {
			// Same key, update value
			return t.hashNode(encodeLeaf(nibbles, value))
		}

		// Create a branch node
		branch := make([][]byte, 17)
		for i := range 17 {
			branch[i] = nil
		}

		if commonLen == len(existingNibbles) {
			branch[16] = existingValue
		} else {
			branch[existingNibbles[commonLen]] = t.hashNode(encodeLeaf(existingNibbles[commonLen+1:], existingValue))
		}

		if commonLen == len(nibbles) {
			branch[16] = value
		} else {
			branch[nibbles[commonLen]] = t.hashNode(encodeLeaf(nibbles[commonLen+1:], value))
		}

		branchNode := t.hashNode(encodeBranch(branch))

		if commonLen > 0 {
			return t.hashNode(encodeExtension(nibbles[:commonLen], branchNode))
		}
		return branchNode

	case nodeTypeExtension:
		extNibbles := decoded[0]
		childRef := decoded[1]

		commonLen := commonPrefixLen(nibbles, extNibbles)

		if commonLen == len(extNibbles) {
			// Key starts with extension prefix, insert into child
			newChild := t.insert(childRef, nibbles[commonLen:], value)
			return t.hashNode(encodeExtension(extNibbles, newChild))
		}

		// Split the extension
		branch := make([][]byte, 17)
		for i := range 17 {
			branch[i] = nil
		}

		if commonLen+1 == len(extNibbles) {
			branch[extNibbles[commonLen]] = childRef
		} else {
			branch[extNibbles[commonLen]] = t.hashNode(encodeExtension(extNibbles[commonLen+1:], childRef))
		}

		if commonLen == len(nibbles) {
			branch[16] = value
		} else {
			branch[nibbles[commonLen]] = t.hashNode(encodeLeaf(nibbles[commonLen+1:], value))
		}

		branchNode := t.hashNode(encodeBranch(branch))

		if commonLen > 0 {
			return t.hashNode(encodeExtension(nibbles[:commonLen], branchNode))
		}
		return branchNode

	case nodeTypeBranch:
		if len(nibbles) == 0 {
			existing := t.resolveNode(node)
			_, branchData := decodeNode(existing)
			branch := decodeBranchRefs(branchData, existing)
			branch[16] = value
			return t.hashNode(encodeBranch(branch))
		}

		existing := t.resolveNode(node)
		_, branchData := decodeNode(existing)
		branch := decodeBranchRefs(branchData, existing)

		idx := nibbles[0]
		branch[idx] = t.insert(branch[idx], nibbles[1:], value)
		return t.hashNode(encodeBranch(branch))
	}

	return t.hashNode(encodeLeaf(nibbles, value))
}

func (t *mpt) hashNode(encoded []byte) []byte {
	if len(encoded) < 32 {
		return encoded
	}
	hash := keccak256(encoded)
	t.db[string(hash)] = encoded
	return hash
}

func (t *mpt) resolveNode(ref []byte) []byte {
	if len(ref) == 32 {
		if data, ok := t.db[string(ref)]; ok {
			return data
		}
		return nil
	}
	return ref
}

// --- Node types ---

const (
	nodeTypeLeaf      = 0
	nodeTypeExtension = 1
	nodeTypeBranch    = 2
)

func decodeNode(data []byte) (int, [][]byte) {
	items := rlpDecodeList(data)
	if len(items) == 17 {
		return nodeTypeBranch, items
	}
	if len(items) == 2 {
		prefix := items[0]
		nibbles := compactToNibbles(prefix)
		if len(nibbles) > 0 && (nibbles[0]&0x02) != 0 {
			// Leaf (flag bit 1 set)
			return nodeTypeLeaf, [][]byte{nibbles[1:], items[1]}
		}
		// Extension
		return nodeTypeExtension, [][]byte{nibbles[1:], items[1]}
	}
	return -1, nil
}

func decodeBranchRefs(_ [][]byte, rawNode []byte) [][]byte {
	branch := make([][]byte, 17)
	// Re-decode to get the raw RLP items including embedded nodes
	rawItems := rlpDecodeListRaw(rawNode)
	for i := range min(17, len(rawItems)) {
		if len(rawItems[i]) == 0 || (len(rawItems[i]) == 1 && rawItems[i][0] == 0x80) {
			branch[i] = nil
		} else {
			branch[i] = rawItems[i]
		}
	}
	return branch
}

// --- Compact (hex-prefix) encoding ---

func bytesToNibbles(data []byte) []byte {
	nibbles := make([]byte, len(data)*2)
	for i, b := range data {
		nibbles[i*2] = b >> 4
		nibbles[i*2+1] = b & 0x0f
	}
	return nibbles
}

func nibblesToCompact(nibbles []byte, isLeaf bool) []byte {
	flag := byte(0)
	if isLeaf {
		flag = 2
	}

	var compact []byte
	if len(nibbles)%2 == 1 {
		// Odd length: first nibble goes into first byte with flag
		compact = append(compact, (flag+1)<<4|nibbles[0])
		nibbles = nibbles[1:]
	} else {
		compact = append(compact, flag<<4)
	}

	for i := 0; i < len(nibbles); i += 2 {
		compact = append(compact, nibbles[i]<<4|nibbles[i+1])
	}
	return compact
}

func compactToNibbles(compact []byte) []byte {
	if len(compact) == 0 {
		return nil
	}

	flag := compact[0] >> 4
	var nibbles []byte

	// First nibble is the flag itself
	nibbles = append(nibbles, flag)

	if flag&0x01 == 1 {
		// Odd: lower nibble of first byte is data
		nibbles = append(nibbles, compact[0]&0x0f)
	}

	for _, b := range compact[1:] {
		nibbles = append(nibbles, b>>4, b&0x0f)
	}
	return nibbles
}

func encodeLeaf(nibbles, value []byte) []byte {
	key := nibblesToCompact(nibbles, true)
	items := [][]byte{
		rlpEncodeBytes(key),
		rlpEncodeBytes(value),
	}
	return rlpEncodeListFromRLP(items)
}

func encodeExtension(nibbles, childRef []byte) []byte {
	key := nibblesToCompact(nibbles, false)
	var childRLP []byte
	if len(childRef) == 32 {
		childRLP = rlpEncodeBytes(childRef)
	} else {
		childRLP = childRef // Already RLP encoded
	}
	items := [][]byte{
		rlpEncodeBytes(key),
		childRLP,
	}
	return rlpEncodeListFromRLP(items)
}

func encodeBranch(children [][]byte) []byte {
	var items [][]byte
	for i := range 16 {
		if children[i] == nil {
			items = append(items, []byte{0x80}) // RLP empty string
		} else if len(children[i]) == 32 {
			items = append(items, rlpEncodeBytes(children[i]))
		} else {
			items = append(items, children[i]) // Inline node
		}
	}
	// Value slot (index 16)
	if children[16] == nil {
		items = append(items, []byte{0x80})
	} else {
		items = append(items, rlpEncodeBytes(children[16]))
	}
	return rlpEncodeListFromRLP(items)
}

func commonPrefixLen(a, b []byte) int {
	maxLen := min(len(a), len(b))
	for i := range maxLen {
		if a[i] != b[i] {
			return i
		}
	}
	return maxLen
}

// --- RLP decoding ---

func rlpDecodeList(data []byte) [][]byte {
	if len(data) == 0 {
		return nil
	}

	_, payload := rlpDecodeListPayload(data)
	if payload == nil {
		return nil
	}

	var items [][]byte
	offset := 0
	for offset < len(payload) {
		item, consumed := rlpDecodeItem(payload[offset:])
		items = append(items, item)
		offset += consumed
	}
	return items
}

func rlpDecodeListRaw(data []byte) [][]byte {
	if len(data) == 0 {
		return nil
	}

	_, payload := rlpDecodeListPayload(data)
	if payload == nil {
		return nil
	}

	var items [][]byte
	offset := 0
	for offset < len(payload) {
		raw, consumed := rlpDecodeItemRaw(payload[offset:])
		items = append(items, raw)
		offset += consumed
	}
	return items
}

func rlpDecodeListPayload(data []byte) (headerLen int, payload []byte) {
	if len(data) == 0 {
		return 0, nil
	}
	prefix := data[0]
	if prefix >= 0xc0 && prefix <= 0xf7 {
		length := int(prefix - 0xc0)
		if 1+length > len(data) {
			return 0, nil
		}
		return 1, data[1 : 1+length]
	}
	if prefix > 0xf7 {
		lenOfLen := int(prefix - 0xf7)
		if 1+lenOfLen > len(data) {
			return 0, nil
		}
		length := decodeUintBE(data[1 : 1+lenOfLen])
		headerLen = 1 + lenOfLen
		if headerLen+length > len(data) {
			return 0, nil
		}
		return headerLen, data[headerLen : headerLen+length]
	}
	return 0, nil
}

func rlpDecodeItem(data []byte) (value []byte, consumed int) {
	if len(data) == 0 {
		return nil, 0
	}
	prefix := data[0]

	// Single byte
	if prefix < 0x80 {
		return data[:1], 1
	}

	// Short string (0-55 bytes)
	if prefix <= 0xb7 {
		length := int(prefix - 0x80)
		consumed = 1 + length
		if consumed > len(data) {
			return nil, consumed
		}
		return data[1:consumed], consumed
	}

	// Long string
	if prefix <= 0xbf {
		lenOfLen := int(prefix - 0xb7)
		length := decodeUintBE(data[1 : 1+lenOfLen])
		consumed = 1 + lenOfLen + length
		if consumed > len(data) {
			return nil, consumed
		}
		return data[1+lenOfLen : consumed], consumed
	}

	// Short list (0-55 bytes)
	if prefix <= 0xf7 {
		length := int(prefix - 0xc0)
		consumed = 1 + length
		return data[1:consumed], consumed
	}

	// Long list
	lenOfLen := int(prefix - 0xf7)
	length := decodeUintBE(data[1 : 1+lenOfLen])
	consumed = 1 + lenOfLen + length
	if consumed > len(data) {
		return nil, consumed
	}
	return data[1+lenOfLen : consumed], consumed
}

func rlpDecodeItemRaw(data []byte) (raw []byte, consumed int) {
	if len(data) == 0 {
		return nil, 0
	}
	prefix := data[0]

	if prefix < 0x80 {
		return data[:1], 1
	}
	if prefix <= 0xb7 {
		length := int(prefix - 0x80)
		consumed = 1 + length
		if consumed > len(data) {
			return nil, consumed
		}
		return data[1:consumed], consumed
	}
	if prefix <= 0xbf {
		lenOfLen := int(prefix - 0xb7)
		length := decodeUintBE(data[1 : 1+lenOfLen])
		consumed = 1 + lenOfLen + length
		if consumed > len(data) {
			return nil, consumed
		}
		return data[1+lenOfLen : consumed], consumed
	}
	if prefix <= 0xf7 {
		length := int(prefix - 0xc0)
		consumed = 1 + length
		if consumed > len(data) {
			return nil, consumed
		}
		return data[:consumed], consumed
	}
	lenOfLen := int(prefix - 0xf7)
	length := decodeUintBE(data[1 : 1+lenOfLen])
	consumed = 1 + lenOfLen + length
	if consumed > len(data) {
		return nil, consumed
	}
	return data[:consumed], consumed
}

func decodeUintBE(data []byte) int {
	result := 0
	for _, b := range data {
		result = result<<8 | int(b)
	}
	return result
}
