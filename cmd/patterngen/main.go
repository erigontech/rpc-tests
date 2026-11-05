package main

import (
	"context"
	"fmt"
	"log"
	"maps"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"syscall"

	"github.com/erigontech/rpc-tests/internal/perf"
	"github.com/erigontech/rpc-tests/internal/vegetagen"
	"github.com/urfave/cli/v2"
)

func main() {
	if err := newApp().Run(os.Args); err != nil {
		log.Fatal(err)
	}
}

func newApp() *cli.App {
	return &cli.App{
		Name:  "rpc_pattern_gen",
		Usage: "Generate vegeta pattern files for rpc_perf from the transactions and blocks of recent blocks",
		// The root only runs with --profile, so it checks --out itself: a required root flag would also be required by eth_call.
		Flags: append(commonFlags(false),
			&cli.StringFlag{Name: "profile", Usage: "Profile file with the [methods] and their weights, and an optional [contracts] filter"},
		),
		Action: func(c *cli.Context) error {
			if !c.IsSet("profile") {
				return cli.ShowAppHelp(c)
			}
			if !c.IsSet("out") {
				return fmt.Errorf("required flag \"out\" not set")
			}
			profile, err := vegetagen.LoadProfile(c.String("profile"))
			if err != nil {
				return err
			}
			specs, err := profile.Specs()
			if err != nil {
				return err
			}
			return generate(c, specs, &profile)
		},
		Commands: []*cli.Command{
			txCallCommand(vegetagen.EthCall{}, "Generate eth_call requests with the call arguments of each transaction"),
			txCallCommand(vegetagen.CreateAccessList{}, "Generate eth_createAccessList requests with the call arguments of each transaction"),
			{
				Name:  "eth_getLogs",
				Usage: "Generate eth_getLogs requests whose filters match the logs of each block, so none returns empty",
				Flags: append(commonFlags(true),
					&cli.Uint64Flag{Name: "range", Value: vegetagen.DefaultLogsRange, Usage: "Widest block range of a request, around the block of its log"},
				),
				Action: func(c *cli.Context) error {
					if c.Uint64("range") == 0 {
						return fmt.Errorf("invalid range 0: want a positive number of blocks")
					}
					return generate(c, []vegetagen.Spec{{Gen: vegetagen.GetLogs{Range: c.Uint64("range")}, Weight: 1}}, nil)
				},
			}, {
				Name:  "tx-list",
				Usage: "Write the txs that use the most gas in some blocks as a tx list for eth_call or eth_createAccessList --txs",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "url", Value: "http://localhost:8545", Usage: "Node used to read the blocks and their receipts"},
					&cli.IntFlag{Name: "top-gas", Required: true, Usage: "Number of txs to write: the successful txs that use the most gas, the heaviest first"},
					&cli.Uint64Flag{Name: "blocks", Usage: "Read this many blocks, from the head downwards"},
					&cli.Uint64Flag{Name: "start-block", Usage: "Read the blocks from this one up to --end-block or for --max-blocks blocks, instead of those below the head"},
					&cli.Uint64Flag{Name: "end-block", Usage: "With --start-block, the last block of the range"},
					&cli.Uint64Flag{Name: "max-blocks", Usage: "With --start-block, the length of the range"},
					&cli.Uint64Flag{Name: "sample-blocks", Usage: "With --start-block, read only this many distinct blocks of the range, drawn at random with a fixed seed"},
					&cli.IntFlag{Name: "workers", Value: 16, Usage: "Concurrent requests to the node"},
					&cli.StringFlag{Name: "out", Required: true, Usage: "Output file: one \"<name> <hash>\" per line"},
				},
				Action: writeTxList,
			}},
	}
}

// txCallCommand is the subcommand of a per-tx method built from the call arguments of each tx: from the
// blocks below the head, from a block range or from a tx list.
func txCallCommand(gen vegetagen.Generator, usage string) *cli.Command {
	return &cli.Command{
		Name:  gen.Method(),
		Usage: usage,
		Flags: append(commonFlags(true),
			&cli.StringFlag{Name: "txs", Usage: "File with one \"<name> <hash>\" per line: one request per tx, in this order, with the name as its id, instead of reading blocks"},
		),
		Action: func(c *cli.Context) error {
			return generate(c, []vegetagen.Spec{{Gen: gen, Weight: 1}}, nil)
		},
	}
}

// writeTxList writes the heaviest txs of the blocks as a tx list.
func writeTxList(c *cli.Context) error {
	blockRange, err := parseBlockRange(c)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	list, err := vegetagen.TopGasTxs(ctx, vegetagen.TxListConfig{
		URL:     c.String("url"),
		Blocks:  c.Uint64("blocks"),
		Range:   blockRange,
		Top:     c.Int("top-gas"),
		Workers: c.Int("workers"),
	})
	if err != nil {
		return err
	}
	if err := vegetagen.WriteGasTxList(c.String("out"), list); err != nil {
		return err
	}
	fmt.Printf("tx-list: %d blocks scanned (%d..%d), %d txs written to %s\n", list.Scanned, list.First, list.Last, len(list.Txs), c.String("out"))
	return nil
}

func commonFlags(outRequired bool) []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: "url", Value: "http://localhost:8545", Usage: "Node used to read blocks and verify requests"},
		&cli.StringFlag{Name: "target-url", Value: "http://localhost:8545", Usage: "URL written in each vegeta target (rpc_perf replaces localhost with --rpc-client-address); tar output only"},
		&cli.Uint64Flag{Name: "blocks", Usage: "Read this many blocks, from the head downwards (alternative to --counts, one method only)"},
		&cli.StringFlag{Name: "block-tag", Value: string(vegetagen.TagLatest), Usage: "State of each request: latest, head (head number pinned at generation) or parent (block before the transaction)"},
		&cli.BoolFlag{Name: "verify", Usage: "Run each request on the node and count the failures"},
		&cli.StringFlag{Name: "keep", Value: string(vegetagen.KeepOK), Usage: "With --verify, the requests to write: ok or all"},
		&cli.StringFlag{Name: "fees", Value: "drop", Usage: "Gas price of each request: drop (no fee, the node skips the base fee and balance checks) or keep (the tx gas price)"},
		&cli.IntFlag{Name: "counts", Usage: "Read blocks from the head downwards until this many requests are written (alternative to --blocks)"},
		&cli.IntFlag{Name: "min-count", Usage: "Fail if fewer requests are written"},
		&cli.Uint64Flag{Name: "max-blocks", Value: vegetagen.DefaultMaxBlocks, Usage: "With a profile, the window of recent blocks all requests are drawn from, with repetitions; with --start-block, the length of the range"},
		&cli.Uint64Flag{Name: "start-block", Usage: "Read the blocks from this one up to --end-block or for --max-blocks blocks, whichever ends first, instead of those below the head"},
		&cli.Uint64Flag{Name: "end-block", Usage: "With --start-block, the last block of the range"},
		&cli.Uint64Flag{Name: "sample-blocks", Usage: "With --start-block, read only this many distinct blocks of the range, drawn at random with a fixed seed"},
		&cli.IntFlag{Name: "per-block", Usage: "Take at most this many txs (or logs for eth_getLogs) of each block, picked at random with the block number as seed (subcommands only; default: all)"},
		&cli.IntFlag{Name: "workers", Value: 16, Usage: "Concurrent requests to the node"},
		&cli.StringFlag{Name: "name", Usage: "Name of the vegeta file in the tar, the rpc_perf --test-type (default: the method, or mixed for several); tar output only"},
		&cli.StringFlag{Name: "description", Usage: "Description written in the header of a .jsonl output, read by rpc_perf into its JSON report (default: built from the parameters); .jsonl output only"},
		&cli.StringFlag{Name: "out", Required: outRequired, Usage: "Output file: a vegeta tar usable as rpc_perf --pattern-file, gzipped if it ends in .tar.gz or .tgz, or one JSON-RPC request per line if it ends in .jsonl"},
		&cli.BoolFlag{Name: "verbose", Aliases: []string{"v"}, Usage: "Print the verification statistics by error kind"},
	}
}

// generate writes the pattern of specs. With a profile the requests are drawn from the window of recent
// blocks even when the profile has one method.
func generate(c *cli.Context, specs []vegetagen.Spec, profile *vegetagen.Profile) error {
	window := profile != nil
	var contracts []string
	if profile != nil {
		contracts = profile.Contracts
	}
	tag, err := vegetagen.ParseBlockTag(c.String("block-tag"))
	if err != nil {
		return err
	}
	keep := vegetagen.Keep(c.String("keep"))
	if keep != vegetagen.KeepOK && keep != vegetagen.KeepAll {
		return fmt.Errorf("invalid keep %q: want ok or all", keep)
	}
	jsonl := strings.HasSuffix(c.String("out"), ".jsonl")
	if jsonl {
		for _, name := range []string{"target-url", "name"} {
			if c.IsSet(name) {
				return fmt.Errorf("--%s applies to a vegeta tar only, not to a .jsonl output", name)
			}
		}
	} else if c.IsSet("description") {
		return fmt.Errorf("--description applies to a .jsonl output only: a tar has no header")
	}
	blockRange, err := parseBlockRange(c)
	if err != nil {
		return err
	}
	var txs []vegetagen.NamedTx
	if c.IsSet("txs") {
		if txs, err = vegetagen.LoadTxList(c.String("txs")); err != nil {
			return err
		}
	}
	if blockRange == nil && txs == nil && (c.Uint64("blocks") == 0) == (c.Int("counts") == 0) {
		return fmt.Errorf("set exactly one of --blocks or --counts")
	}
	if c.Uint64("blocks") > 0 && (len(specs) > 1 || window) {
		return fmt.Errorf("a profile needs --counts, since its requests are drawn from the window of --max-blocks blocks")
	}
	fees := c.String("fees")
	if fees != "drop" && fees != "keep" {
		return fmt.Errorf("invalid fees %q: want drop or keep", fees)
	}
	cfg := vegetagen.Config{
		URL:       c.String("url"),
		TargetURL: c.String("target-url"),
		Blocks:    c.Uint64("blocks"),
		Tag:       tag,
		Verify:    c.Bool("verify"),
		Keep:      keep,
		KeepFees:  fees == "keep",
		Count:     c.Int("counts"),
		MinCount:  c.Int("min-count"),
		Contracts: contracts,
		Name:      c.String("name"),
		MaxBlocks: c.Uint64("max-blocks"),
		Window:    window,
		Range:     blockRange,
		PerBlock:  c.Int("per-block"),
		Workers:   c.Int("workers"),
		Out:       c.String("out"),
		Txs:       txs,
	}
	label := specs[0].Gen.Method()
	if len(specs) > 1 {
		label = "mixed"
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if jsonl {
		description := c.String("description")
		if description == "" {
			txList := ""
			if c.IsSet("txs") {
				txList = filepath.Base(c.String("txs"))
			}
			description = vegetagen.Description(cfg, specs, profile, txList)
		}
		cfg.Header = &perf.PatternInfo{
			Description: description,
			Network:     vegetagen.Network(ctx, cfg.URL),
			Generator:   generatorCommand(c),
		}
	}
	sum, err := vegetagen.Run(ctx, cfg, specs)
	if c.Bool("verbose") {
		printSummary(label, cfg, specs, sum)
	}
	if err != nil {
		return err
	}
	if txs != nil {
		for _, name := range slices.Sorted(maps.Keys(sum.FailedTxs)) {
			fmt.Printf("%s: %s\n", name, sum.FailedTxs[name])
		}
		fmt.Printf("%s: %d txs, %d requests written to %s\n", label, len(txs), sum.Written, cfg.Out)
		return nil
	}
	fmt.Printf("%s: %d blocks scanned (%d..%d), %d requests written to %s\n",
		label, sum.BlocksScanned, sum.FirstBlock, sum.LastBlock, sum.Written, cfg.Out)
	return nil
}

// generatorCommand returns the command line of the pattern for its header, rebuilt from the flags set in
// their order of definition, without those that depend on the machine or are in the header already: the node
// URLs, the output file and the description.
func generatorCommand(c *cli.Context) string {
	skip := map[string]bool{"url": true, "target-url": true, "out": true, "description": true}
	parts := []string{c.App.Name}
	flags := c.App.Flags
	if c.Command != nil && c.Command.Name != "" && c.Command.Name != c.App.Name {
		parts = append(parts, c.Command.Name)
		flags = c.Command.Flags
	}
	for _, f := range flags {
		name := f.Names()[0]
		if skip[name] || !c.IsSet(name) {
			continue
		}
		if _, ok := f.(*cli.BoolFlag); ok {
			parts = append(parts, "--"+name)
			continue
		}
		parts = append(parts, "--"+name, fmt.Sprint(c.Value(name)))
	}
	return strings.Join(parts, " ")
}

// parseBlockRange returns the range of --start-block, or nil without it. It checks which flags are set, not
// their values, since --max-blocks has a default.
func parseBlockRange(c *cli.Context) (*vegetagen.BlockRange, error) {
	if !c.IsSet("start-block") {
		for _, name := range []string{"end-block", "sample-blocks"} {
			if c.IsSet(name) {
				return nil, fmt.Errorf("--%s needs --start-block", name)
			}
		}
		return nil, nil
	}
	if !c.IsSet("end-block") && !c.IsSet("max-blocks") {
		return nil, fmt.Errorf("--start-block needs --end-block or --max-blocks")
	}
	if c.IsSet("blocks") {
		return nil, fmt.Errorf("--blocks cannot be used with --start-block: the range sets the blocks to read")
	}
	r := &vegetagen.BlockRange{From: c.Uint64("start-block"), To: c.Uint64("end-block"), Sample: c.Uint64("sample-blocks")}
	if c.IsSet("max-blocks") {
		r.Len = c.Uint64("max-blocks")
	}
	return r, nil
}

func printSummary(label string, cfg vegetagen.Config, specs []vegetagen.Spec, sum vegetagen.Summary) {
	fees := "drop"
	if cfg.KeepFees {
		fees = "keep"
	}
	fmt.Printf("%s (block-tag=%s, fees=%s): %d generated\n", label, cfg.Tag, fees, sum.Generated)
	printCounts("  ", sum.Generated, sum.OK, sum.Errors)
	if len(specs) > 1 {
		for _, sp := range specs {
			name := sp.Name
			if name == "" {
				name = sp.Gen.Method()
			}
			ms := sum.Methods[name]
			fmt.Printf("  %s: %d generated, %d written\n", name, ms.Generated, ms.Written)
			printCounts("    ", ms.Generated, ms.OK, ms.Errors)
		}
	}
	fmt.Printf("written: %d (keep=%s)\n", sum.Written, cfg.Keep)
}

func printCounts(indent string, generated, ok int, errors map[string]int) {
	if generated == 0 || (ok == 0 && len(errors) == 0) {
		return
	}
	pct := func(n int) float64 { return 100 * float64(n) / float64(generated) }
	fmt.Printf("%s%-50s %8d (%.1f%%)\n", indent, "ok", ok, pct(ok))
	msgs := make([]string, 0, len(errors))
	for msg := range errors {
		msgs = append(msgs, msg)
	}
	sort.Slice(msgs, func(i, j int) bool { return errors[msgs[i]] > errors[msgs[j]] })
	for _, msg := range msgs {
		fmt.Printf("%s%-50s %8d (%.1f%%)\n", indent, msg, errors[msg], pct(errors[msg]))
	}
}
