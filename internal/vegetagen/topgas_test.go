package vegetagen

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func hashOf(n uint64) string { return fmt.Sprintf("0x%064x", n) }

func TestTopGasTxsTakesTheHeaviestSuccessfulTxs(t *testing.T) {
	_, url := startNode(t)

	got, err := TopGasTxs(context.Background(), TxListConfig{URL: url, Blocks: 5, Top: 3, Workers: 2})
	if err != nil {
		t.Fatalf("TopGasTxs: %v", err)
	}
	want := []GasTx{{Hash: hashOf(1000), GasUsed: 100000, Block: 100}, {Hash: hashOf(990), GasUsed: 99000, Block: 99}, {Hash: hashOf(980), GasUsed: 98000, Block: 98}}
	if fmt.Sprint(got.Txs) != fmt.Sprint(want) {
		t.Errorf("got %v, want %v", got.Txs, want)
	}
	if got.Scanned != 5 || got.First != 96 || got.Last != 100 {
		t.Errorf("scanned %d blocks %d..%d, want 5 blocks 96..100", got.Scanned, got.First, got.Last)
	}
}

func TestTopGasTxsReadsABlockRange(t *testing.T) {
	_, url := startNode(t)

	got, err := TopGasTxs(context.Background(), TxListConfig{URL: url, Range: &BlockRange{From: 10, To: 20}, Top: 2, Workers: 2})
	if err != nil {
		t.Fatalf("TopGasTxs: %v", err)
	}
	if len(got.Txs) != 2 || got.Txs[0].Block != 20 || got.Txs[1].Block != 19 {
		t.Errorf("got %v, want the first txs of blocks 20 and 19", got.Txs)
	}
	if got.Scanned != 11 || got.First != 10 || got.Last != 20 {
		t.Errorf("scanned %d blocks %d..%d, want 11 blocks 10..20", got.Scanned, got.First, got.Last)
	}
}

func TestTopGasTxsRejectsInvalidSettings(t *testing.T) {
	_, url := startNode(t)
	for name, cfg := range map[string]TxListConfig{
		"no top":             {URL: url, Blocks: 5},
		"no blocks or range": {URL: url, Top: 3},
		"blocks and range":   {URL: url, Blocks: 5, Range: &BlockRange{From: 10, To: 20}, Top: 3},
	} {
		if _, err := TopGasTxs(context.Background(), cfg); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestWriteGasTxListIsATxList(t *testing.T) {
	path := filepath.Join(t.TempDir(), "candidates.txt")
	list := GasTxList{Txs: []GasTx{{Hash: hashOf(1), GasUsed: 1309930, Block: 100}, {Hash: hashOf(2), GasUsed: 21000, Block: 99}}, Scanned: 5, First: 96, Last: 100}

	if err := WriteGasTxList(path, list); err != nil {
		t.Fatalf("WriteGasTxList: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "# the 2 txs that use the most gas in 5 blocks between 96 and 100\n") {
		t.Errorf("header: %s", data)
	}
	txs, err := ParseTxList(strings.NewReader(string(data)))
	if err != nil {
		t.Fatalf("not a tx list: %v\n%s", err, data)
	}
	want := []NamedTx{{Name: "1-1.31Mgas", Hash: hashOf(1)}, {Name: "2-0.02Mgas", Hash: hashOf(2)}}
	if fmt.Sprint(txs) != fmt.Sprint(want) {
		t.Errorf("got %v, want %v", txs, want)
	}
}
