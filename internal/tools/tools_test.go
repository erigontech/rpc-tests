package tools

import (
	"testing"
)

func TestIsSubcommand(t *testing.T) {
	known := []string{
		"block-by-number",
		"empty-blocks",
		"filter-changes",
		"latest-block-logs",
		"subscriptions",
		"graphql",
		"replay-request",
		"replay-tx",
		"scan-block-receipts",
	}
	for _, name := range known {
		if !IsSubcommand(name) {
			t.Errorf("IsSubcommand(%q) = false, want true", name)
		}
	}

	unknown := []string{"-c", "--help", "foo", "run", ""}
	for _, name := range unknown {
		if IsSubcommand(name) {
			t.Errorf("IsSubcommand(%q) = true, want false", name)
		}
	}
}

func TestCommandsCount(t *testing.T) {
	cmds := Commands()
	if len(cmds) != 9 {
		t.Errorf("Commands() returned %d commands, want 9", len(cmds))
	}
}

func TestCommandsHaveAction(t *testing.T) {
	for _, cmd := range Commands() {
		if cmd.Action == nil {
			t.Errorf("command %q has nil Action", cmd.Name)
		}
		if cmd.Usage == "" {
			t.Errorf("command %q has empty Usage", cmd.Name)
		}
	}
}
