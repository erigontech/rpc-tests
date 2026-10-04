package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sort"
	"syscall"

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
			return generate(c, specs, profile.Contracts)
		},
		Commands: []*cli.Command{{
			Name:  "eth_call",
			Usage: "Generate eth_call requests with the call arguments of each transaction",
			Flags: commonFlags(true),
			Action: func(c *cli.Context) error {
				return generate(c, []vegetagen.Spec{{Gen: vegetagen.EthCall{}, Weight: 1}}, nil)
			},
		}},
	}
}

func commonFlags(outRequired bool) []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: "url", Value: "http://localhost:8545", Usage: "Node used to read blocks and verify requests"},
		&cli.StringFlag{Name: "target-url", Value: "http://localhost:8545", Usage: "URL written in each vegeta target (rpc_perf replaces localhost with --rpc-client-address)"},
		&cli.Uint64Flag{Name: "blocks", Usage: "Read this many blocks, from the head downwards (alternative to --counts, one method only)"},
		&cli.StringFlag{Name: "block-tag", Value: string(vegetagen.TagLatest), Usage: "State of each request: latest, head (head number pinned at generation) or parent (block before the transaction)"},
		&cli.BoolFlag{Name: "verify", Usage: "Run each request on the node and count the failures"},
		&cli.StringFlag{Name: "keep", Value: string(vegetagen.KeepOK), Usage: "With --verify, the requests to write: ok or all"},
		&cli.StringFlag{Name: "fees", Value: "drop", Usage: "Gas price of each request: drop (no fee, the node skips the base fee and balance checks) or keep (the tx gas price)"},
		&cli.IntFlag{Name: "counts", Usage: "Read blocks from the head downwards until this many requests are written (alternative to --blocks)"},
		&cli.IntFlag{Name: "min-count", Usage: "Fail if fewer requests are written"},
		&cli.Uint64Flag{Name: "max-blocks", Value: vegetagen.DefaultMaxBlocks, Usage: "With several methods, the window of recent blocks all requests are drawn from, with repetitions"},
		&cli.IntFlag{Name: "workers", Value: 16, Usage: "Concurrent requests to the node"},
		&cli.StringFlag{Name: "name", Usage: "Name of the vegeta file in the tar, the rpc_perf --test-type (default: the method, or mixed for several)"},
		&cli.StringFlag{Name: "out", Required: outRequired, Usage: "Output tar file, usable as rpc_perf --pattern-file"},
		&cli.BoolFlag{Name: "verbose", Aliases: []string{"v"}, Usage: "Print the verification statistics by error kind"},
	}
}

func generate(c *cli.Context, specs []vegetagen.Spec, contracts []string) error {
	tag, err := vegetagen.ParseBlockTag(c.String("block-tag"))
	if err != nil {
		return err
	}
	keep := vegetagen.Keep(c.String("keep"))
	if keep != vegetagen.KeepOK && keep != vegetagen.KeepAll {
		return fmt.Errorf("invalid keep %q: want ok or all", keep)
	}
	if (c.Uint64("blocks") == 0) == (c.Int("counts") == 0) {
		return fmt.Errorf("set exactly one of --blocks or --counts")
	}
	if c.Uint64("blocks") > 0 && len(specs) > 1 {
		return fmt.Errorf("a profile with several methods needs --counts, since the shares apply to a total")
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
		Workers:   c.Int("workers"),
		Out:       c.String("out"),
	}
	label := specs[0].Gen.Method()
	if len(specs) > 1 {
		label = "mixed"
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	sum, err := vegetagen.Run(ctx, cfg, specs)
	if c.Bool("verbose") {
		printSummary(label, cfg, specs, sum)
	}
	if err != nil {
		return err
	}
	fmt.Printf("%s: %d blocks scanned (%d..%d), %d requests written to %s\n",
		label, sum.BlocksScanned, sum.FirstBlock, sum.LastBlock, sum.Written, cfg.Out)
	return nil
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
