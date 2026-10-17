package vegetagen

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"slices"
	"strings"
	"testing"
)

func TestGeneratorRequests(t *testing.T) {
	block := &Block{Number: "0x64", Hash: "0xbb"}
	txn := &Transaction{Hash: "0xaa", From: "0x01", To: strPtr("0x02"), Gas: "0x5208", Value: "0x0", Input: "0x"}
	item := Item{Block: block, BlockNum: 100, Txn: txn}
	params := Params{BlockParam: "latest"}

	tests := []struct {
		method  string
		options map[string]string
		params  Params
		want    string
	}{
		{"eth_estimateGas", nil, params, `{"jsonrpc":"2.0","method":"eth_estimateGas","params":[{"from":"0x01","to":"0x02","gas":"0x5208","value":"0x0"},"latest"],"id":1}`},
		{"eth_getTransactionReceipt", nil, params, `{"jsonrpc":"2.0","method":"eth_getTransactionReceipt","params":["0xaa"],"id":1}`},
		{"eth_getTransactionByHash", nil, params, `{"jsonrpc":"2.0","method":"eth_getTransactionByHash","params":["0xaa"],"id":1}`},
		{"eth_getBalance", nil, params, `{"jsonrpc":"2.0","method":"eth_getBalance","params":["0x01","latest"],"id":1}`},
		{"eth_getTransactionCount", nil, params, `{"jsonrpc":"2.0","method":"eth_getTransactionCount","params":["0x01","latest"],"id":1}`},
		{"eth_getBlockByNumber", nil, params, `{"jsonrpc":"2.0","method":"eth_getBlockByNumber","params":["0x64",false],"id":1}`},
		{"eth_getBlockByNumber", map[string]string{"full": "true"}, params, `{"jsonrpc":"2.0","method":"eth_getBlockByNumber","params":["0x64",true],"id":1}`},
		{"eth_getBlockByHash", nil, params, `{"jsonrpc":"2.0","method":"eth_getBlockByHash","params":["0xbb",false],"id":1}`},
		{"eth_getBlockReceipts", nil, params, `{"jsonrpc":"2.0","method":"eth_getBlockReceipts","params":["0x64"],"id":1}`},
		{"debug_traceBlockByHash", nil, params, `{"jsonrpc":"2.0","method":"debug_traceBlockByHash","params":["0xbb"],"id":1}`},
		{"debug_traceBlockByHash", map[string]string{"tracer": "callTracer"}, params, `{"jsonrpc":"2.0","method":"debug_traceBlockByHash","params":["0xbb",{"tracer":"callTracer"}],"id":1}`},
		{"eth_blockNumber", nil, params, `{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}`},
		{"eth_chainId", nil, params, `{"jsonrpc":"2.0","method":"eth_chainId","params":[],"id":1}`},
		{"net_listening", nil, params, `{"jsonrpc":"2.0","method":"net_listening","params":[],"id":1}`},
		{"eth_gasPrice", nil, params, `{"jsonrpc":"2.0","method":"eth_gasPrice","params":[],"id":1}`},
		{"eth_maxPriorityFeePerGas", nil, params, `{"jsonrpc":"2.0","method":"eth_maxPriorityFeePerGas","params":[],"id":1}`},
	}
	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			gen, err := NewGenerator(tt.method, tt.options)
			if err != nil {
				t.Fatalf("NewGenerator: %v", err)
			}
			got, err := gen.Request(item, tt.params)
			if err != nil {
				t.Fatalf("Request: %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("got  %s\nwant %s", got, tt.want)
			}
		})
	}
}

func TestGeneratorKinds(t *testing.T) {
	kinds := map[string]Kind{
		"eth_call": PerTx, "eth_estimateGas": PerTx, "eth_getTransactionReceipt": PerTx, "eth_getBalance": PerTx,
		"eth_getBlockByNumber": PerBlock, "eth_getLogs": PerLog, "debug_traceBlockByHash": PerBlock,
		"eth_blockNumber": Constant, "net_listening": Constant,
		"eth_gasPrice": Constant, "eth_maxPriorityFeePerGas": Constant,
	}
	for method, want := range kinds {
		gen, err := NewGenerator(method, nil)
		if err != nil {
			t.Fatalf("NewGenerator(%s): %v", method, err)
		}
		if gen.Kind() != want {
			t.Errorf("%s kind %v, want %v", method, gen.Kind(), want)
		}
	}
}

func TestGetLogsRequest(t *testing.T) {
	lg := &Log{Address: "0x02", Topics: []string{"0x7e", "0x01", "0x02"}, TxHash: "0xaa", Index: "0x3"}
	item := Item{Block: &Block{Number: "0x64"}, BlockNum: 100, Log: lg}
	gen, err := NewGenerator("eth_getLogs", map[string]string{"range": "1"})
	if err != nil {
		t.Fatalf("NewGenerator: %v", err)
	}
	first, err := gen.Request(item, Params{Head: 100})
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if again, _ := gen.Request(item, Params{Head: 100}); string(again) != string(first) {
		t.Errorf("not deterministic: %s then %s", first, again)
	}
	if !strings.Contains(string(first), `"fromBlock":"0x64","toBlock":"0x64"`) {
		t.Errorf("range=1: want the log's block only, got %s", first)
	}
	shapes := map[string]bool{}
	for i := range 200 {
		lg.Index = fmt.Sprintf("0x%x", i)
		req, _ := gen.Request(item, Params{Head: 100})
		var r struct {
			Params []struct {
				Address string `json:"address"`
				Topics  []any  `json:"topics"`
			} `json:"params"`
		}
		if err := json.Unmarshal(req, &r); err != nil {
			t.Fatal(err)
		}
		shapes[fmt.Sprint(r.Params[0].Address != "", r.Params[0].Topics)] = true
	}
	if len(shapes) != len(logShapes(lg.Topics, "")) {
		t.Errorf("got %d filter shapes, want %d: %v", len(shapes), len(logShapes(lg.Topics, "")), shapes)
	}
}

func TestGetLogsFilterRestrictsTheShapes(t *testing.T) {
	lg := &Log{Address: "0x02", Topics: []string{"0x7e", "0x01", "0x02"}, TxHash: "0xaa"}
	item := Item{Block: &Block{Number: "0x64"}, BlockNum: 100, Log: lg}
	for filter, check := range map[string]func(address string, topics []any) bool{
		FilterAddress:       func(a string, tp []any) bool { return a != "" && len(tp) == 0 },
		FilterAddressTopics: func(a string, tp []any) bool { return a != "" && len(tp) > 0 },
		FilterTopics:        func(a string, tp []any) bool { return a == "" && len(tp) >= 2 },
	} {
		gen, err := NewGenerator("eth_getLogs", map[string]string{"filter": filter})
		if err != nil {
			t.Fatalf("NewGenerator(filter=%s): %v", filter, err)
		}
		for i := range 50 {
			lg.Index = fmt.Sprintf("0x%x", i)
			req, err := gen.Request(item, Params{Head: 100})
			if err != nil {
				t.Fatalf("filter=%s: %v", filter, err)
			}
			var r struct {
				Params []struct {
					Address string `json:"address"`
					Topics  []any  `json:"topics"`
				} `json:"params"`
			}
			if err := json.Unmarshal(req, &r); err != nil {
				t.Fatal(err)
			}
			if !check(r.Params[0].Address, r.Params[0].Topics) {
				t.Errorf("filter=%s: wrong shape %s", filter, req)
			}
		}
	}
	gen, _ := NewGenerator("eth_getLogs", map[string]string{"filter": FilterTopics})
	if usableLog(gen, Log{Topics: []string{"0x7e"}}) {
		t.Error("filter=topics: a log with one topic is not usable, topic0 alone is left out")
	}
	if !usableLog(gen, Log{Topics: []string{"0x7e", "0x01"}}) {
		t.Error("filter=topics: a log with two topics is usable")
	}
}

// orBlock has several logs per topic position, so or=N can fill lists from the same block.
func orBlock() *Block {
	return &Block{Number: "0x64", Logs: []Log{
		{Address: "0xa1", Topics: []string{"0x10", "0x20", "0x30"}},
		{Address: "0xa2", Topics: []string{"0x11", "0x21", "0x31"}},
		{Address: "0xa3", Topics: []string{"0x12", "0x22", "0x32"}},
		{Address: "0xa4", Topics: []string{"0x13", "0x23", "0x33"}},
		{Address: "0xa5", Topics: []string{"0x14"}},
		{Address: "0xa1", Topics: []string{"0x10", "0x20", "0x30"}, Removed: true},
	}}
}

// alternatives returns the values of a filter field, a single value or a list of alternatives.
func alternatives(t *testing.T, v any) []string {
	t.Helper()
	switch x := v.(type) {
	case nil:
		return nil
	case string:
		return []string{x}
	case []any:
		out := make([]string, len(x))
		for i, e := range x {
			out[i] = e.(string)
		}
		return out
	}
	t.Fatalf("unexpected filter value %#v", v)
	return nil
}

func TestGetLogsOrListsHoldTheLogValueAndOthersOfTheBlock(t *testing.T) {
	block := orBlock()
	gen, err := NewGenerator("eth_getLogs", map[string]string{"or": "4"})
	if err != nil {
		t.Fatalf("NewGenerator: %v", err)
	}
	pool := func(pos int) map[string]bool { // address at -1, topic i at i
		m := map[string]bool{}
		for _, lg := range block.Logs {
			switch {
			case lg.Removed:
			case pos < 0:
				m[lg.Address] = true
			case pos < len(lg.Topics):
				m[lg.Topics[pos]] = true
			}
		}
		return m
	}
	longest := 0
	for i := range 400 {
		lg := block.Logs[i%4]
		lg.TxHash, lg.Index = "0xaa", fmt.Sprintf("0x%x", i)
		item := Item{Block: block, BlockNum: 100, Log: &lg}
		req, err := gen.Request(item, Params{Head: 100})
		if err != nil {
			t.Fatalf("Request: %v", err)
		}
		if again, _ := gen.Request(item, Params{Head: 100}); string(again) != string(req) {
			t.Fatalf("not deterministic: %s then %s", req, again)
		}
		var r struct {
			Params []struct {
				Address any   `json:"address"`
				Topics  []any `json:"topics"`
			} `json:"params"`
		}
		if err := json.Unmarshal(req, &r); err != nil {
			t.Fatal(err)
		}
		fields := map[int]any{-1: r.Params[0].Address}
		for pos, v := range r.Params[0].Topics {
			fields[pos] = v
		}
		for pos, v := range fields {
			got := alternatives(t, v)
			if len(got) == 0 {
				continue
			}
			want := lg.Address
			if pos >= 0 {
				want = lg.Topics[pos]
			}
			seen := map[string]bool{}
			for _, a := range got {
				if seen[a] {
					t.Errorf("%s: %s repeated", req, a)
				}
				seen[a] = true
				if !pool(pos)[a] {
					t.Errorf("%s: %s is not a value of the block at position %d", req, a, pos)
				}
			}
			if !seen[want] {
				t.Errorf("%s: the log value %s is missing, the request could return nothing", req, want)
			}
			if len(got) > 4 {
				t.Errorf("%s: %d alternatives, want at most 4", req, len(got))
			}
			longest = max(longest, len(got))
		}
	}
	if longest < 2 {
		t.Errorf("or=4: no request has a list of alternatives")
	}
}

func TestGetLogsOrOneKeepsSingleValues(t *testing.T) {
	block := orBlock()
	lg := block.Logs[0]
	lg.TxHash = "0xaa"
	def, _ := NewGenerator("eth_getLogs", nil)
	one, err := NewGenerator("eth_getLogs", map[string]string{"or": "1"})
	if err != nil {
		t.Fatalf("NewGenerator(or=1): %v", err)
	}
	for i := range 100 {
		lg.Index = fmt.Sprintf("0x%x", i)
		item := Item{Block: block, BlockNum: 100, Log: &lg}
		a, _ := def.Request(item, Params{Head: 100})
		b, _ := one.Request(item, Params{Head: 100})
		if string(a) != string(b) {
			t.Fatalf("or=1 differs from the default: %s vs %s", b, a)
		}
		if strings.Contains(string(a), "[[") || strings.Contains(string(a), `"address":[`) {
			t.Fatalf("default: want single values, got %s", a)
		}
	}
}

func TestGetLogsOptions(t *testing.T) {
	for _, bad := range []map[string]string{{"range": "0"}, {"range": "x"}, {"full": "true"}, {"filter": "topic0"}, {"or": "0"}, {"or": "x"}} {
		if _, err := NewGenerator("eth_getLogs", bad); err == nil {
			t.Errorf("options %v: want an error", bad)
		}
	}
}

func TestGetLogsOrListsKeepToTheContracts(t *testing.T) {
	block := orBlock()
	contracts := map[string]bool{"0xa1": true, "0xa2": true}
	gen, err := NewGenerator("eth_getLogs", map[string]string{"or": "4"})
	if err != nil {
		t.Fatalf("NewGenerator: %v", err)
	}
	outside := []string{"0xa3", "0xa4", "0xa5", "0x12", "0x13", "0x14", "0x22", "0x23", "0x32", "0x33"}
	lists := 0
	for i := range 200 {
		lg := block.Logs[i%2]
		lg.TxHash, lg.Index = "0xaa", fmt.Sprintf("0x%x", i)
		req, err := gen.Request(Item{Block: block, BlockNum: 100, Log: &lg}, Params{Head: 100, Contracts: contracts})
		if err != nil {
			t.Fatalf("Request: %v", err)
		}
		for _, v := range outside {
			if strings.Contains(string(req), `"`+v+`"`) {
				t.Errorf("%s: %s is not a value of the logs of the contracts", req, v)
			}
		}
		if strings.Contains(string(req), `",[`) || strings.Contains(string(req), `"address":[`) || strings.Contains(string(req), `[["`) {
			lists++
		}
	}
	if lists == 0 {
		t.Error("or=4 with contracts: no request has a list of alternatives")
	}
}

func TestLogShapes(t *testing.T) {
	tests := []struct {
		filter string
		topics int
		want   []string
	}{
		{"", 0, []string{"address"}},
		{"", 1, []string{"address", "address+topic0"}},
		{"", 2, []string{"address", "address+topic0", "address+topic0+topic1", "topic0+topic1"}},
		{"", 3, []string{"address", "address+topic0", "address+topic0+topic1", "topic0+topic1", "address+topic0+topic2", "topic0+topic2"}},
		{FilterAddress, 0, []string{"address"}},
		{FilterAddress, 3, []string{"address"}},
		{FilterAddressTopics, 0, nil},
		{FilterAddressTopics, 1, []string{"address+topic0"}},
		{FilterAddressTopics, 3, []string{"address+topic0", "address+topic0+topic1", "address+topic0+topic2"}},
		{FilterTopics, 1, nil},
		{FilterTopics, 2, []string{"topic0+topic1"}},
		{FilterTopics, 4, []string{"topic0+topic1", "topic0+topic2"}},
	}
	for _, tt := range tests {
		topics := make([]string, tt.topics)
		if got := logShapes(topics, tt.filter); !slices.Equal(got, tt.want) {
			t.Errorf("logShapes(%d topics, %q) = %v, want %v", tt.topics, tt.filter, got, tt.want)
		}
	}
}

func TestBlockValues(t *testing.T) {
	if got := blockValues(nil, -1, nil); got != nil {
		t.Errorf("nil block: got %v, want nil", got)
	}
	b := &Block{Logs: []Log{
		{Address: "0xA1", Topics: []string{"0x10", "0x20"}},
		{Address: "0xa1", Topics: []string{"0x11"}},                // same address, other case
		{Address: "0xa2", Topics: []string{"0x10"}},                // same topic0
		{Address: "0xa3", Topics: []string{"0x12"}, Removed: true}, // removed: never a value
		{Address: "0xa4"}, // no topics
	}}
	tests := []struct {
		pos       int
		contracts map[string]bool
		want      []string
	}{
		{-1, nil, []string{"0xA1", "0xa2", "0xa4"}},
		{0, nil, []string{"0x10", "0x11"}},
		{1, nil, []string{"0x20"}},
		{2, nil, nil},
		{-1, map[string]bool{"0xa1": true}, []string{"0xA1"}},
		{0, map[string]bool{"0xa2": true, "0xa4": true}, []string{"0x10"}},
		{0, map[string]bool{"0xa3": true}, nil},
	}
	for _, tt := range tests {
		if got := blockValues(b, tt.pos, tt.contracts); !slices.Equal(got, tt.want) {
			t.Errorf("blockValues(pos %d, contracts %v) = %v, want %v", tt.pos, tt.contracts, got, tt.want)
		}
	}
}

func TestOrList(t *testing.T) {
	pool := []string{"0x01", "0x02", "0X03", "0x04"}
	rng := rand.New(rand.NewSource(1)) //nolint:gosec
	for range 100 {
		if v := orList(rng, 1, "0x03", pool); v != "0x03" {
			t.Fatalf("maxLen 1: got %v, want the value alone", v)
		}
	}
	lengths := map[int]bool{}
	for range 1000 {
		switch v := orList(rng, 8, "0x03", pool).(type) {
		case string:
			if v != "0x03" {
				t.Fatalf("single value %s, want 0x03", v)
			}
			lengths[1] = true
		case []string:
			if len(v) < 2 || len(v) > len(pool) || v[0] != "0x03" {
				t.Fatalf("list %v: want 0x03 first and at most the %d values of the pool", v, len(pool))
			}
			for i, a := range v[1:] {
				if strings.EqualFold(a, "0x03") || slices.Contains(v[i+2:], a) {
					t.Fatalf("list %v: %s repeated", v, a)
				}
			}
			lengths[len(v)] = true
		}
	}
	if !lengths[1] || !lengths[2] || !lengths[len(pool)] {
		t.Errorf("lengths drawn %v: want short and long lists, capped by the pool", lengths)
	}
	if v := orList(rand.New(rand.NewSource(2)), 1000, "0x03", nil); v != "0x03" && !slices.Equal(v.([]string), []string{"0x03"}) { //nolint:gosec
		t.Errorf("empty pool: got %v, want the value only", v)
	}
}

func TestGetLogsGeneratorOptions(t *testing.T) {
	tests := []struct {
		options map[string]string
		want    GetLogs
	}{
		{nil, GetLogs{Range: DefaultLogsRange, Or: 1}},
		{map[string]string{"range": "7"}, GetLogs{Range: 7, Or: 1}},
		{map[string]string{"filter": FilterAddress}, GetLogs{Range: DefaultLogsRange, Filter: FilterAddress, Or: 1}},
		{map[string]string{"filter": FilterAddressTopics}, GetLogs{Range: DefaultLogsRange, Filter: FilterAddressTopics, Or: 1}},
		{map[string]string{"filter": FilterTopics, "or": "16", "range": "10"}, GetLogs{Range: 10, Filter: FilterTopics, Or: 16}},
	}
	for _, tt := range tests {
		gen, err := NewGenerator("eth_getLogs", tt.options)
		if err != nil {
			t.Fatalf("options %v: %v", tt.options, err)
		}
		if gen != tt.want {
			t.Errorf("options %v: got %+v, want %+v", tt.options, gen, tt.want)
		}
	}
}

func TestUsableLog(t *testing.T) {
	noTopics := Log{Address: "0xa1"}
	if !usableLog(EthCall{}, noTopics) {
		t.Error("a generator other than eth_getLogs: want every log usable")
	}
	if !usableLog(GetLogs{}, noTopics) || !usableLog(GetLogs{Filter: FilterAddress}, noTopics) {
		t.Error("no filter or filter=address: a log without topics is usable by its address")
	}
	if usableLog(GetLogs{Filter: FilterAddressTopics}, noTopics) {
		t.Error("filter=address+topics: a log without topics is not usable")
	}
	if !usableLog(GetLogs{Filter: FilterAddressTopics}, Log{Topics: []string{"0x10"}}) {
		t.Error("filter=address+topics: a log with topic0 is usable")
	}
}

func TestGetLogsRequestFailsWithoutAShape(t *testing.T) {
	lg := &Log{Address: "0xa1", Topics: []string{"0x10"}, TxHash: "0xaa", Index: "0x0"}
	if _, err := (GetLogs{Range: 1, Filter: FilterTopics, Or: 1}).Request(Item{BlockNum: 1, Log: lg}, Params{Head: 1}); err == nil {
		t.Error("filter=topics on a log with topic0 only: want an error")
	}
}
