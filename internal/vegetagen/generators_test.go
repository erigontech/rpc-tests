package vegetagen

import (
	"testing"
)

func TestGeneratorRequests(t *testing.T) {
	block := &Block{Number: "0x64", Hash: "0xbb"}
	txn := &Transaction{Hash: "0xaa", From: "0x01", To: strPtr("0x02"), Gas: "0x5208", Value: "0x0", Input: "0x"}
	item := Item{Block: block, BlockNum: 100, Txn: txn}
	params := Params{BlockParam: "latest"}
	contracts := Params{BlockParam: "latest", Contracts: []string{"0x02", "0x03"}}

	tests := []struct {
		method  string
		options map[string]string
		params  Params
		want    string
	}{
		{"eth_getTransactionReceipt", nil, params, `{"jsonrpc":"2.0","method":"eth_getTransactionReceipt","params":["0xaa"],"id":1}`},
		{"eth_getTransactionByHash", nil, params, `{"jsonrpc":"2.0","method":"eth_getTransactionByHash","params":["0xaa"],"id":1}`},
		{"eth_getBalance", nil, params, `{"jsonrpc":"2.0","method":"eth_getBalance","params":["0x01","latest"],"id":1}`},
		{"eth_getTransactionCount", nil, params, `{"jsonrpc":"2.0","method":"eth_getTransactionCount","params":["0x01","latest"],"id":1}`},
		{"eth_getBlockByNumber", nil, params, `{"jsonrpc":"2.0","method":"eth_getBlockByNumber","params":["0x64",false],"id":1}`},
		{"eth_getBlockByNumber", map[string]string{"full": "true"}, params, `{"jsonrpc":"2.0","method":"eth_getBlockByNumber","params":["0x64",true],"id":1}`},
		{"eth_getBlockByHash", nil, params, `{"jsonrpc":"2.0","method":"eth_getBlockByHash","params":["0xbb",false],"id":1}`},
		{"eth_getBlockReceipts", nil, params, `{"jsonrpc":"2.0","method":"eth_getBlockReceipts","params":["0x64"],"id":1}`},
		{"eth_getLogs", nil, params, `{"jsonrpc":"2.0","method":"eth_getLogs","params":[{"fromBlock":"0x64","toBlock":"0x64"}],"id":1}`},
		{"eth_getLogs", nil, contracts, `{"jsonrpc":"2.0","method":"eth_getLogs","params":[{"fromBlock":"0x64","toBlock":"0x64","address":["0x02","0x03"]}],"id":1}`},
		{"debug_traceBlockByHash", nil, params, `{"jsonrpc":"2.0","method":"debug_traceBlockByHash","params":["0xbb"],"id":1}`},
		{"debug_traceBlockByHash", map[string]string{"tracer": "callTracer"}, params, `{"jsonrpc":"2.0","method":"debug_traceBlockByHash","params":["0xbb",{"tracer":"callTracer"}],"id":1}`},
		{"eth_blockNumber", nil, params, `{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}`},
		{"eth_chainId", nil, params, `{"jsonrpc":"2.0","method":"eth_chainId","params":[],"id":1}`},
		{"net_listening", nil, params, `{"jsonrpc":"2.0","method":"net_listening","params":[],"id":1}`},
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
		"eth_call": PerTx, "eth_getTransactionReceipt": PerTx, "eth_getBalance": PerTx,
		"eth_getBlockByNumber": PerBlock, "eth_getLogs": PerBlock, "debug_traceBlockByHash": PerBlock,
		"eth_blockNumber": Constant, "net_listening": Constant,
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
