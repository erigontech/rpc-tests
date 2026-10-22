package vegetagen

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/erigontech/rpc-tests/internal/rpc"
)

// NamedTx is one line of a tx list: a label and the hash of the tx.
type NamedTx struct {
	Name string
	Hash string
}

var txHashRe = regexp.MustCompile(`^0x[0-9a-fA-F]{64}$`)

func LoadTxList(path string) ([]NamedTx, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	txs, err := ParseTxList(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return txs, nil
}

// ParseTxList reads one "<name> <hash>" per line; # starts a comment. The names must be unique,
// since they label the requests.
func ParseTxList(r io.Reader) ([]NamedTx, error) {
	var txs []NamedTx
	seen := map[string]bool{}
	sc := bufio.NewScanner(r)
	for lineNum := 1; sc.Scan(); lineNum++ {
		line, _, _ := strings.Cut(sc.Text(), "#")
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if len(fields) != 2 {
			return nil, fmt.Errorf("line %d: expected \"<name> <hash>\", got %q", lineNum, strings.TrimSpace(line))
		}
		name, hash := fields[0], fields[1]
		if !txHashRe.MatchString(hash) {
			return nil, fmt.Errorf("line %d: invalid tx hash %q", lineNum, hash)
		}
		if seen[name] {
			return nil, fmt.Errorf("line %d: duplicated name %s", lineNum, name)
		}
		seen[name] = true
		txs = append(txs, NamedTx{Name: name, Hash: hash})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(txs) == 0 {
		return nil, errors.New("no txs")
	}
	return txs, nil
}

// runTxs builds one request per tx of cfg.Txs, in their order, with the name of the tx as the request id.
func runTxs(ctx context.Context, cfg Config, specs []Spec, sum Summary) (Summary, error) {
	if len(specs) != 1 || specs[0].Gen.Kind() != PerTx {
		return sum, errors.New("a tx list needs one per-tx method")
	}
	if cfg.Blocks > 0 || cfg.Count > 0 || cfg.Range != nil || cfg.PerBlock > 0 || len(cfg.Contracts) > 0 || cfg.Window {
		return sum, errors.New("a tx list sets the requests: unset blocks, count, range, per-block, contracts and window")
	}
	sp := specs[0]
	n := newNode(cfg.URL)
	head, _, err := rpc.GetLatestBlockNumber(ctx, n.client, n.target)
	if err != nil {
		return sum, fmt.Errorf("reading head block: %w", err)
	}
	workers := max(cfg.Workers, 1)
	txs := make([]Transaction, len(cfg.Txs))
	if err := parallel(len(cfg.Txs), workers, func(i int) error {
		txn, err := n.transaction(ctx, cfg.Txs[i])
		txs[i] = txn
		return err
	}); err != nil {
		return sum, err
	}
	reqs := make([][]byte, len(txs))
	for i, txn := range txs {
		blockNum, err := strconv.ParseUint(strings.TrimPrefix(*txn.BlockNumber, "0x"), 16, 64)
		if err != nil {
			return sum, fmt.Errorf("tx %s: invalid block number %q", cfg.Txs[i].Name, *txn.BlockNumber)
		}
		if !cfg.KeepFees {
			txn.GasPrice = ""
		}
		req, err := sp.Gen.Request(Item{BlockNum: blockNum, Txn: &txn}, Params{BlockParam: blockParam(cfg.Tag, blockNum, head), Head: head})
		if err != nil {
			return sum, fmt.Errorf("building %s for tx %s: %w", sp.Gen.Method(), cfg.Txs[i].Name, err)
		}
		if reqs[i], err = withID(req, cfg.Txs[i].Name); err != nil {
			return sum, err
		}
	}
	var failures []string
	if cfg.Verify {
		if failures, err = n.verify(ctx, reqs, false, workers); err != nil {
			return sum, err
		}
	}
	ms := sum.Methods[sp.name()]
	sum.FailedTxs = map[string]string{}
	var written [][]byte
	for i, req := range reqs {
		ms.Generated++
		sum.Generated++
		if cfg.Verify {
			if failures[i] == "" {
				ms.OK++
				sum.OK++
			} else {
				ms.Errors[failures[i]]++
				sum.Errors[failures[i]]++
				sum.FailedTxs[cfg.Txs[i].Name] = failures[i]
				if cfg.Keep == KeepOK {
					continue
				}
			}
		}
		written = append(written, req)
		ms.Written++
		sum.Written++
	}
	return sum, write(cfg, specs, sum, [][][]byte{written})
}

// transaction reads tx from the node and fails when it is unknown or still pending.
func (n node) transaction(ctx context.Context, tx NamedTx) (Transaction, error) {
	var resp struct {
		Result *Transaction `json:"result"`
		Error  *rpcError    `json:"error"`
	}
	req, err := marshalRequest("eth_getTransactionByHash", tx.Hash)
	if err != nil {
		return Transaction{}, err
	}
	if _, err := n.client.Call(ctx, n.target, req, &resp); err != nil {
		return Transaction{}, fmt.Errorf("reading tx %s %s: %w", tx.Name, tx.Hash, err)
	}
	if resp.Error != nil {
		return Transaction{}, fmt.Errorf("reading tx %s %s: %s", tx.Name, tx.Hash, resp.Error.Message)
	}
	if resp.Result == nil {
		return Transaction{}, fmt.Errorf("tx %s %s not found", tx.Name, tx.Hash)
	}
	if resp.Result.BlockNumber == nil {
		return Transaction{}, fmt.Errorf("tx %s %s is pending", tx.Name, tx.Hash)
	}
	return *resp.Result, nil
}

// withID replaces the id of a JSON-RPC request with id.
func withID(req []byte, id string) ([]byte, error) {
	var r struct {
		Jsonrpc string          `json:"jsonrpc"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
		ID      any             `json:"id"`
	}
	if err := json.Unmarshal(req, &r); err != nil {
		return nil, err
	}
	r.ID = id
	return json.Marshal(r)
}
