package vegetagen

import (
	"testing"
)

func strPtr(s string) *string { return &s }

func TestEthCallRequest(t *testing.T) {
	txn := Transaction{
		From:     "0x7b9d4d8772b8705ddc7456daf821c3022dda0504",
		To:       strPtr("0x3fc91a3afd70395cd496c647d5a6cc9d4b2b7fad"),
		Gas:      "0x55730",
		GasPrice: "0x582e2a2ee",
		Value:    "0x0",
		Input:    "0x24856bc3",
	}
	tests := []struct {
		name       string
		txn        Transaction
		blockParam string
		want       string
	}{
		{
			name:       "call at a block number",
			txn:        txn,
			blockParam: "0x1520e63",
			want:       `{"jsonrpc":"2.0","method":"eth_call","params":[{"from":"0x7b9d4d8772b8705ddc7456daf821c3022dda0504","to":"0x3fc91a3afd70395cd496c647d5a6cc9d4b2b7fad","gas":"0x55730","gasPrice":"0x582e2a2ee","value":"0x0","data":"0x24856bc3"},"0x1520e63"],"id":1}`,
		},
		{
			name:       "call at latest",
			txn:        txn,
			blockParam: "latest",
			want:       `{"jsonrpc":"2.0","method":"eth_call","params":[{"from":"0x7b9d4d8772b8705ddc7456daf821c3022dda0504","to":"0x3fc91a3afd70395cd496c647d5a6cc9d4b2b7fad","gas":"0x55730","gasPrice":"0x582e2a2ee","value":"0x0","data":"0x24856bc3"},"latest"],"id":1}`,
		},
		{
			name:       "contract creation has no to and empty input has no data",
			txn:        Transaction{From: "0x01", Gas: "0x5208", GasPrice: "0x1", Value: "0x2", Input: "0x"},
			blockParam: "latest",
			want:       `{"jsonrpc":"2.0","method":"eth_call","params":[{"from":"0x01","gas":"0x5208","gasPrice":"0x1","value":"0x2"},"latest"],"id":1}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := EthCall{}.Request(tt.txn, tt.blockParam)
			if err != nil {
				t.Fatalf("Request: %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("got  %s\nwant %s", got, tt.want)
			}
		})
	}
}

func TestBlockParam(t *testing.T) {
	tests := []struct {
		tag      BlockTag
		blockNum uint64
		head     uint64
		want     string
	}{
		{TagLatest, 100, 120, "latest"},
		{TagHead, 100, 120, "0x78"},
		{TagParent, 100, 120, "0x63"},
	}
	for _, tt := range tests {
		t.Run(string(tt.tag), func(t *testing.T) {
			if got := blockParam(tt.tag, tt.blockNum, tt.head); got != tt.want {
				t.Errorf("got %s, want %s", got, tt.want)
			}
		})
	}
}

func TestParseBlockTag(t *testing.T) {
	for _, s := range []string{"latest", "head", "parent"} {
		if _, err := ParseBlockTag(s); err != nil {
			t.Errorf("ParseBlockTag(%q): %v", s, err)
		}
	}
	if _, err := ParseBlockTag("pending"); err == nil {
		t.Error("ParseBlockTag(pending): want error")
	}
}
