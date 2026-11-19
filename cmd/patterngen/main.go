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
	"strconv"
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
	app := &cli.App{
		Name:  "rpc_pattern_gen",
		Usage: "Generate vegeta pattern files for rpc_perf from the transactions and blocks of recent blocks",
		// --client values hold commas; every slice flag is repeated instead.
		DisableSliceFlagSeparator: true,
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
				Name:  "eth_subscribe",
				Usage: "Generate the eth_subscribe requests each WebSocket connection of rpc_perf sends, logs filters matching the logs of recent blocks",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "url", Value: "http://localhost:8545", Usage: "Node used to read the logs of recent blocks"},
					&cli.IntFlag{Name: vegetagen.SubscribeNewHeads, Usage: "Number of newHeads subscriptions"},
					&cli.IntFlag{Name: vegetagen.SubscribeLogsType, Usage: "Number of logs subscriptions, each with the filter of a log of a recent block"},
					&cli.IntFlag{Name: vegetagen.SubscribeTransactionReceipts, Usage: "Number of transactionReceipts subscriptions"},
					&cli.StringFlag{Name: "logs-filter", Usage: "Filter family of the logs subscriptions: address, address+topics, topics, or all for every log (default: any shape)"},
					&cli.StringSliceFlag{Name: "client", Usage: "Client class instead of the counts, name:share:type=count,...,pick=n (repeatable): its share of the connections and the subscriptions each sends, " +
						"types newHeads, logs, logs-all and transactionReceipts; with pick, each connection sends n of the logs subscriptions of the class, in turn"},
					&cli.Uint64Flag{Name: "max-blocks", Value: vegetagen.DefaultMaxBlocks, Usage: "Window of recent blocks the logs filters are drawn from"},
					&cli.IntFlag{Name: "workers", Value: 16, Usage: "Concurrent requests to the node"},
					&cli.StringFlag{Name: "description", Usage: "Description written in the header (default: built from the parameters)"},
					&cli.StringFlag{Name: "out", Required: true, Usage: "Output .jsonl file: one eth_subscribe request per line"},
				},
				Action: writeSubscriptions,
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
	for _, method := range vegetagen.Methods() {
		if !slices.ContainsFunc(app.Commands, func(cmd *cli.Command) bool { return cmd.Name == method }) {
			app.Commands = append(app.Commands, methodCommand(method))
		}
	}
	return app
}

// kindUsage tells what each request of a generator kind is built from.
var kindUsage = map[vegetagen.Kind]string{
	vegetagen.PerTx:    "one per transaction",
	vegetagen.PerBlock: "one per block",
	vegetagen.PerLog:   "one per log",
	vegetagen.Constant: "all the same, with no parameters taken from the chain",
}

// methodCommand is the subcommand of a method with no command of its own: its options are those of
// the method in a profile, e.g. full=true.
func methodCommand(method string) *cli.Command {
	gen, err := vegetagen.NewGenerator(method, nil)
	if err != nil {
		panic(err) // a method of vegetagen.Methods() always builds with no options
	}
	return &cli.Command{
		Name:  method,
		Usage: fmt.Sprintf("Generate %s requests, %s", method, kindUsage[gen.Kind()]),
		Flags: append(commonFlags(true),
			&cli.StringSliceFlag{Name: "option", Usage: "Option of the method as in a profile, key=value, e.g. full=true or tag=latest (repeatable)"},
		),
		Action: func(c *cli.Context) error {
			options := map[string]string{}
			for _, opt := range c.StringSlice("option") {
				key, value, ok := strings.Cut(opt, "=")
				if !ok || key == "" {
					return fmt.Errorf("invalid option %q: want key=value", opt)
				}
				if _, dup := options[key]; dup {
					return fmt.Errorf("option %q set twice", key)
				}
				options[key] = value
			}
			// An explicit --block-tag latest replaces the block number of a block lookup with "latest", as
			// tag=latest. The default does not, so without the flag each request reads its own block.
			if c.IsSet("block-tag") && c.String("block-tag") == string(vegetagen.TagLatest) && vegetagen.TakesTagOption(method) {
				options["tag"] = string(vegetagen.TagLatest)
			}
			gen, err := vegetagen.NewGenerator(method, options)
			if err != nil {
				return err
			}
			name := []string{method}
			for _, key := range slices.Sorted(maps.Keys(options)) {
				name = append(name, key+"="+options[key])
			}
			return generate(c, []vegetagen.Spec{{Name: strings.Join(name, " "), Gen: gen, Weight: 1}}, nil)
		},
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

// writeSubscriptions writes the subscriptions of the counts of each type, interleaved, or those of each
// client class in turn.
func writeSubscriptions(c *cli.Context) error {
	var (
		specs   []vegetagen.Spec
		clients []perf.ClientClass
		parts   []string
	)
	if c.IsSet("client") {
		for _, typ := range []string{vegetagen.SubscribeNewHeads, vegetagen.SubscribeLogsType, vegetagen.SubscribeTransactionReceipts} {
			if c.IsSet(typ) {
				return fmt.Errorf("--%s and --client both set the subscriptions: set one", typ)
			}
		}
		for _, arg := range c.StringSlice("client") {
			class, counts, err := parseClient(arg)
			if err != nil {
				return err
			}
			if slices.ContainsFunc(clients, func(cc perf.ClientClass) bool { return cc.Name == class.Name }) {
				return fmt.Errorf("--client %s: duplicate class %q", arg, class.Name)
			}
			own, names, err := subscriptionSpecs(class.Name, counts, c.String("logs-filter"))
			if err != nil {
				return err
			}
			label := fmt.Sprintf("%s share %g", class.Name, class.Share)
			if class.Pick > 0 {
				label += fmt.Sprintf(", pick %d", class.Pick)
			}
			clients = append(clients, class)
			specs = append(specs, own...)
			parts = append(parts, label+": "+strings.Join(names, ", "))
		}
	} else {
		counts := map[string]int{}
		for _, typ := range []string{vegetagen.SubscribeNewHeads, vegetagen.SubscribeLogsType, vegetagen.SubscribeTransactionReceipts} {
			counts[typ] = c.Int(typ)
		}
		own, names, err := subscriptionSpecs("", counts, c.String("logs-filter"))
		if err != nil {
			return err
		}
		specs, parts = own, names
	}
	if len(specs) == 0 {
		return fmt.Errorf("set at least one of --%s, --%s or --%s, or --client", vegetagen.SubscribeNewHeads, vegetagen.SubscribeLogsType, vegetagen.SubscribeTransactionReceipts)
	}
	total, fromLog := 0, false // fromLog: a logs filter is drawn from a log of the window
	for _, sp := range specs {
		total += int(sp.Weight)
		fromLog = fromLog || sp.Gen.Kind() == vegetagen.PerLog
	}
	cfg := vegetagen.Config{
		URL:       c.String("url"),
		Count:     total,
		MaxBlocks: 1,
		Window:    true,
		Workers:   c.Int("workers"),
		Out:       c.String("out"),
	}
	description := "eth_subscribe on each WebSocket connection: " + strings.Join(parts, ", ")
	if len(clients) > 0 {
		description = "eth_subscribe by client class: " + strings.Join(parts, "; ")
	}
	if fromLog {
		cfg.MaxBlocks = c.Uint64("max-blocks")
		description += fmt.Sprintf("; logs filters from the %d most recent blocks", cfg.MaxBlocks)
	}
	if c.IsSet("description") {
		description = c.String("description")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cfg.Header = &perf.PatternInfo{
		Description: description,
		Network:     vegetagen.Network(ctx, cfg.URL),
		Generator:   generatorCommand(c),
		Clients:     clients,
	}
	sum, err := vegetagen.Run(ctx, cfg, specs)
	if err != nil {
		return err
	}
	fmt.Printf("eth_subscribe: %d subscriptions written to %s\n", sum.Written, cfg.Out)
	return nil
}

// logsAll is the --client key of the logs subscriptions with no filter.
const logsAll = "logs-all"

// subscriptionSpecs returns the specs of the counts by type of the subscriptions of a client class, empty
// without classes, and their names.
func subscriptionSpecs(class string, counts map[string]int, logsFilter string) ([]vegetagen.Spec, []string, error) {
	var (
		specs []vegetagen.Spec
		names []string
	)
	for _, key := range []string{vegetagen.SubscribeNewHeads, vegetagen.SubscribeLogsType, logsAll, vegetagen.SubscribeTransactionReceipts} {
		n := counts[key]
		if n < 0 {
			return nil, nil, fmt.Errorf("invalid %s count %d: want a count", key, n)
		}
		if n == 0 {
			continue
		}
		options := map[string]string{"type": key}
		switch {
		case key == logsAll:
			options = map[string]string{"type": vegetagen.SubscribeLogsType, "filter": vegetagen.FilterAll}
		case key == vegetagen.SubscribeLogsType && logsFilter != "":
			options["filter"] = logsFilter
		}
		gen, err := vegetagen.NewGenerator("eth_subscribe", options)
		if err != nil {
			return nil, nil, err
		}
		name := options["type"]
		if f := options["filter"]; f != "" {
			name += " filter=" + f
		}
		specName := "eth_subscribe " + name
		if class != "" {
			specName += " " + class
		}
		specs = append(specs, vegetagen.Spec{Name: specName, Class: class, Gen: gen, Weight: float64(n)})
		names = append(names, fmt.Sprintf("%d %s", n, name))
	}
	return specs, names, nil
}

// parseClient parses a --client value, name:share:type=count,...,pick=n, into its class and its counts by type.
func parseClient(arg string) (perf.ClientClass, map[string]int, error) {
	fields := strings.SplitN(arg, ":", 3)
	if len(fields) != 3 || fields[0] == "" {
		return perf.ClientClass{}, nil, fmt.Errorf("--client %s: want name:share:type=count[,type=count]", arg)
	}
	class := perf.ClientClass{Name: fields[0]}
	var err error
	if class.Share, err = strconv.ParseFloat(fields[1], 64); err != nil || class.Share <= 0 {
		return class, nil, fmt.Errorf("--client %s: invalid share %q, want a positive number", arg, fields[1])
	}
	counts, logs, total := map[string]int{}, 0, 0
	for item := range strings.SplitSeq(fields[2], ",") {
		key, value, ok := strings.Cut(item, "=")
		n, err := strconv.Atoi(value)
		if !ok || err != nil || n < 0 {
			return class, nil, fmt.Errorf("--client %s: invalid %q, want type=count", arg, item)
		}
		switch key {
		case "pick":
			if class.Pick != 0 {
				return class, nil, fmt.Errorf("--client %s: pick set twice", arg)
			}
			class.Pick = n
			continue
		case vegetagen.SubscribeLogsType, logsAll:
			logs += n
		case vegetagen.SubscribeNewHeads, vegetagen.SubscribeTransactionReceipts:
		default:
			return class, nil, fmt.Errorf("--client %s: unknown type %q, want %s, %s, %s or %s", arg, key,
				vegetagen.SubscribeNewHeads, vegetagen.SubscribeLogsType, logsAll, vegetagen.SubscribeTransactionReceipts)
		}
		if _, dup := counts[key]; dup {
			return class, nil, fmt.Errorf("--client %s: %s set twice", arg, key)
		}
		counts[key] = n
		total += n
	}
	switch {
	case total == 0:
		return class, nil, fmt.Errorf("--client %s: no subscription", arg)
	case class.Pick > logs:
		return class, nil, fmt.Errorf("--client %s: pick %d of %d logs subscriptions", arg, class.Pick, logs)
	}
	return class, counts, nil
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
		&cli.StringFlag{Name: "block-tag", Value: string(vegetagen.TagLatest), Usage: "State of each request: latest, head (head number pinned at generation) or parent (block before the transaction); given explicitly, latest also replaces the block number of the eth_getBlockByNumber and eth_getBlockReceipts subcommands"},
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
		if _, ok := f.(*cli.StringSliceFlag); ok {
			for _, v := range c.StringSlice(name) {
				parts = append(parts, "--"+name, v)
			}
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
