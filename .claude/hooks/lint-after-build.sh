#!/bin/bash
# Runs golangci-lint after go build commands.

INPUT=$(cat)

COMMAND=$(echo "$INPUT" | jq -r '.tool_input.command // empty')

if echo "$COMMAND" | grep -q 'go build'; then
  cd "$CLAUDE_PROJECT_DIR" || exit 0
  OUTPUT=$(golangci-lint run ./... 2>&1)
  EXIT_CODE=$?
  if [ $EXIT_CODE -ne 0 ]; then
    echo "golangci-lint found issues:" >&2
    echo "$OUTPUT" >&2
    exit 2
  fi
fi

exit 0