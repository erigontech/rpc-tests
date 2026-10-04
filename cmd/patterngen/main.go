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
	app := &cli.App{
		Name:  "rpc_pattern_gen",
		Usage: "Generate vegeta pattern files for rpc_perf from the transactions of recent blocks",
		Commands: []*cli.Command{
			generatorCommand(vegetagen.EthCall{}, "Generate eth_call requests with the call arguments of each transaction"),
		},
	}
	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}

func generatorCommand(gen vegetagen.Generator, usage string) *cli.Command {
	return &cli.Command{
		Name:  gen.Method(),
		Usage: usage,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "url", Value: "http://localhost:8545", Usage: "Node used to read blocks and verify requests"},
			&cli.StringFlag{Name: "target-url", Value: "http://localhost:8545", Usage: "URL written in each vegeta target (rpc_perf replaces localhost with --rpc-client-address)"},
			&cli.Uint64Flag{Name: "blocks", Usage: "Read this many blocks, from the head downwards (alternative to --counts)"},
			&cli.StringFlag{Name: "block-tag", Value: string(vegetagen.TagLatest), Usage: "State of each request: latest, head (head number pinned at generation) or parent (block before the transaction)"},
			&cli.BoolFlag{Name: "verify", Usage: "Run each request on the node and count the failures"},
			&cli.StringFlag{Name: "keep", Value: string(vegetagen.KeepOK), Usage: "With --verify, the requests to write: ok or all"},
			&cli.StringFlag{Name: "fees", Value: "drop", Usage: "Gas price of each request: drop (no fee, the node skips the base fee and balance checks) or keep (the tx gas price)"},
			&cli.IntFlag{Name: "counts", Usage: "Read blocks from the head downwards until this many requests are written (alternative to --blocks)"},
			&cli.IntFlag{Name: "min-count", Usage: "Fail if fewer requests are written"},
			&cli.IntFlag{Name: "workers", Value: 16, Usage: "Concurrent requests to the node"},
			&cli.StringFlag{Name: "out", Required: true, Usage: "Output tar file, usable as rpc_perf --pattern-file"},
			&cli.BoolFlag{Name: "verbose", Aliases: []string{"v"}, Usage: "Print the verification statistics by error kind"},
		},
		Action: func(c *cli.Context) error {
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
				Workers:   c.Int("workers"),
				Out:       c.String("out"),
			}
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			sum, err := vegetagen.Run(ctx, cfg, gen)
			if c.Bool("verbose") {
				printSummary(gen.Method(), cfg, sum)
			}
			if err != nil {
				return err
			}
			fmt.Printf("%s: %d blocks scanned (%d..%d), %d requests written to %s\n",
				gen.Method(), sum.BlocksScanned, sum.FirstBlock, sum.LastBlock, sum.Written, cfg.Out)
			return nil
		},
	}
}

func printSummary(method string, cfg vegetagen.Config, sum vegetagen.Summary) {
	fees := "drop"
	if cfg.KeepFees {
		fees = "keep"
	}
	fmt.Printf("%s (block-tag=%s, fees=%s): %d generated\n", method, cfg.Tag, fees, sum.Generated)
	if sum.Generated == 0 {
		return
	}
	pct := func(n int) float64 { return 100 * float64(n) / float64(sum.Generated) }
	if sum.OK > 0 || len(sum.Errors) > 0 {
		fmt.Printf("  %-50s %8d (%.1f%%)\n", "ok", sum.OK, pct(sum.OK))
		msgs := make([]string, 0, len(sum.Errors))
		for msg := range sum.Errors {
			msgs = append(msgs, msg)
		}
		sort.Slice(msgs, func(i, j int) bool { return sum.Errors[msgs[i]] > sum.Errors[msgs[j]] })
		for _, msg := range msgs {
			fmt.Printf("  %-50s %8d (%.1f%%)\n", msg, sum.Errors[msg], pct(sum.Errors[msg]))
		}
	}
	fmt.Printf("written: %d (keep=%s)\n", sum.Written, cfg.Keep)
}
