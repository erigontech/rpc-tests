package vegetagen

import (
	"fmt"
	"testing"
)

// BenchmarkGetLogsOrRequest builds an or-list request for every log of a 1000-log block, as a profile does.
func BenchmarkGetLogsOrRequest(b *testing.B) {
	block := &Block{Number: "0x64"}
	for i := range 1000 {
		block.Logs = append(block.Logs, Log{
			Address: fmt.Sprintf("0x%040x", i%200),
			Topics:  []string{fmt.Sprintf("0x%064x", i%50), fmt.Sprintf("0x%064x", i), fmt.Sprintf("0x%064x", i+1)},
			TxHash:  fmt.Sprintf("0x%064x", i/4),
			Index:   fmt.Sprintf("0x%x", i),
		})
	}
	gen, err := NewGenerator("eth_getLogs", map[string]string{"or": "16"})
	if err != nil {
		b.Fatal(err)
	}
	for b.Loop() {
		for i := range block.Logs {
			if _, err := gen.Request(Item{Block: block, BlockNum: 100, Log: &block.Logs[i]}, Params{Head: 100}); err != nil {
				b.Fatal(err)
			}
		}
	}
}
