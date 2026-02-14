package tools

import (
	"github.com/urfave/cli/v2"
)

// subcommandNames is the set of known subcommand names for fast lookup.
var subcommandNames map[string]bool

func init() {
	subcommandNames = make(map[string]bool)
	for _, cmd := range Commands() {
		subcommandNames[cmd.Name] = true
	}
}

// IsSubcommand returns true if the given name matches a registered subcommand.
func IsSubcommand(name string) bool {
	return subcommandNames[name]
}

// Commands returns all tool subcommands.
func Commands() []*cli.Command {
	return []*cli.Command{
		blockByNumberCommand,
		emptyBlocksCommand,
		filterChangesCommand,
		latestBlockLogsCommand,
		subscriptionsCommand,
		graphqlCommand,
		replayRequestCommand,
		replayTxCommand,
		scanBlockReceiptsCommand,
	}
}
