package vegetagen

import (
	"context"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const (
	hashA = "0x5a158d08067feeffe06fb1930c50c9f82b0c9175a22d6d110c190fe0dd47d1aa"
	hashB = "0xea93481b5f1faf0c45f4569755b5cd8059df3fe2e47f89bca4ef25acaa1ebd7e"
)

func TestParseTxListReadsNameAndHashSkippingComments(t *testing.T) {
	list := "# slow calls\n\n1-div-loop-28M " + hashA + "\n  2-sload-storm\t" + hashB + "  # storage reads\n"
	txs, err := ParseTxList(strings.NewReader(list))
	if err != nil {
		t.Fatalf("ParseTxList: %v", err)
	}
	want := []NamedTx{{Name: "1-div-loop-28M", Hash: hashA}, {Name: "2-sload-storm", Hash: hashB}}
	if len(txs) != len(want) {
		t.Fatalf("got %v, want %v", txs, want)
	}
	for i := range want {
		if txs[i] != want[i] {
			t.Errorf("tx %d: got %v, want %v", i, txs[i], want[i])
		}
	}
}

func TestParseTxListRejectsInvalidLines(t *testing.T) {
	for _, tc := range []struct{ name, list, err string }{
		{"no hash", "1-div-loop\n", "line 1: expected \"<name> <hash>\""},
		{"extra field", "1-div-loop " + hashA + " extra\n", "line 1: expected \"<name> <hash>\""},
		{"short hash", "# c\n1-div-loop 0x5a158d08\n", "line 2: invalid tx hash \"0x5a158d08\""},
		{"duplicated name", "1-a " + hashA + "\n1-a " + hashB + "\n", "line 2: duplicated name 1-a"},
		{"empty", "# nothing\n", "no txs"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseTxList(strings.NewReader(tc.list))
			if err == nil || !strings.Contains(err.Error(), tc.err) {
				t.Errorf("got %v, want an error with %q", err, tc.err)
			}
		})
	}
}

// Tx hashes the fake node treats specially: the first is unknown, the second is sent from failingFrom.
const (
	missingTx = "0x0000000000000000000000000000000000000000000000000000000000000404"
	failingTx = "0x0000000000000000000000000000000000000000000000000000000000000bad"
)

func txsConfig(t *testing.T, url string, txs ...NamedTx) Config {
	cfg := baseConfig(t, url)
	cfg.Blocks, cfg.Verify = 0, false
	cfg.Txs = txs
	cfg.Out = filepath.Join(t.TempDir(), "out.jsonl")
	return cfg
}

func TestRunTxsWritesOneCallPerTxLabelledWithItsName(t *testing.T) {
	_, url := startNode(t)
	cfg := txsConfig(t, url, NamedTx{"2-sload-storm", hashB}, NamedTx{"1-div-loop", hashA})

	sum, err := Run(context.Background(), cfg, single(EthCall{}))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if sum.Written != 2 {
		t.Errorf("written = %d, want 2", sum.Written)
	}
	call := `{"jsonrpc":"2.0","method":"eth_call","params":[{"from":"0x0000000000000000000000000000000000000001",` +
		`"to":"0x0000000000000000000000000000000000000002","gas":"0x5208","value":"0x0","data":"0x12"},"latest"],"id":%q}`
	want := []string{fmt.Sprintf(call, "2-sload-storm"), fmt.Sprintf(call, "1-div-loop")}
	if got := readJSONL(t, cfg.Out); !slices.Equal(got, want) {
		t.Errorf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestRunTxsParentRunsOnTheBlockBeforeTheTx(t *testing.T) {
	_, url := startNode(t)
	cfg := txsConfig(t, url, NamedTx{"1-div-loop", hashA})
	cfg.Tag = TagParent

	if _, err := Run(context.Background(), cfg, single(EthCall{})); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := readJSONL(t, cfg.Out); len(got) != 1 || !strings.HasSuffix(got[0], `,"0x4f"],"id":"1-div-loop"}`) {
		t.Errorf("got %v, want the call at block 0x4f", got)
	}
}

func TestRunTxsFailsOnAnUnknownTx(t *testing.T) {
	_, url := startNode(t)
	cfg := txsConfig(t, url, NamedTx{"1-div-loop", hashA}, NamedTx{"2-gone", missingTx})

	_, err := Run(context.Background(), cfg, single(EthCall{}))
	if err == nil || !strings.Contains(err.Error(), "tx 2-gone "+missingTx+" not found") {
		t.Errorf("got %v, want the unknown tx named", err)
	}
}

func TestRunTxsVerifyDropsAndNamesTheFailingTxs(t *testing.T) {
	_, url := startNode(t)
	cfg := txsConfig(t, url, NamedTx{"1-div-loop", hashA}, NamedTx{"2-broke", failingTx})
	cfg.Verify = true

	sum, err := Run(context.Background(), cfg, single(EthCall{}))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if sum.Generated != 2 || sum.OK != 1 || sum.Written != 1 {
		t.Errorf("summary generated=%d ok=%d written=%d, want 2/1/1", sum.Generated, sum.OK, sum.Written)
	}
	if want := map[string]string{"2-broke": "insufficient funds for gas * price + value"}; !maps.Equal(sum.FailedTxs, want) {
		t.Errorf("failed txs = %v, want %v", sum.FailedTxs, want)
	}
	if got := readJSONL(t, cfg.Out); len(got) != 1 || !strings.Contains(got[0], `"id":"1-div-loop"`) {
		t.Errorf("got %v, want only 1-div-loop", got)
	}
}

func TestRunTxsRejectsTheBlockSettings(t *testing.T) {
	_, url := startNode(t)
	for name, set := range map[string]func(*Config){
		"blocks":    func(c *Config) { c.Blocks = 2 },
		"count":     func(c *Config) { c.Count = 2 },
		"range":     func(c *Config) { c.Range = &BlockRange{From: 1, To: 2} },
		"per-block": func(c *Config) { c.PerBlock = 1 },
		"contracts": func(c *Config) { c.Contracts = []string{"0x0000000000000000000000000000000000000002"} },
		"window":    func(c *Config) { c.Window = true },
	} {
		t.Run(name, func(t *testing.T) {
			cfg := txsConfig(t, url, NamedTx{"1-div-loop", hashA})
			set(&cfg)
			if _, err := Run(context.Background(), cfg, single(EthCall{})); err == nil {
				t.Error("want an error")
			}
		})
	}
	cfg := txsConfig(t, url, NamedTx{"1-div-loop", hashA})
	if _, err := Run(context.Background(), cfg, single(mustGen(t, "eth_getBlockReceipts"))); err == nil {
		t.Error("per-block method: want an error")
	}
}
