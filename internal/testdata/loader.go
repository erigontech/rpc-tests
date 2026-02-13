package testdata

import (
	"archive/tar"
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	jsoniter "github.com/json-iterator/go"

	"github.com/erigontech/rpc-tests/internal/archive"
)

var json = jsoniter.ConfigCompatibleWithStandardLibrary

// IsArchive returns true if the file is not a plain .json file.
func IsArchive(filename string) bool {
	return !strings.HasSuffix(filename, ".json")
}

// LoadFixture loads JSON-RPC commands from a test fixture file.
// Supports .json, .tar, .tar.gz, .tar.bz2 formats via the archive package.
func LoadFixture(path string, sanitizeExt bool, metrics *TestMetrics) ([]JsonRpcCommand, error) {
	if IsArchive(path) {
		return extractJsonCommands(path, sanitizeExt, metrics)
	}
	return readJsonCommands(path, metrics)
}

// readJsonCommands reads JSON-RPC commands from a plain JSON file.
func readJsonCommands(path string, metrics *TestMetrics) ([]JsonRpcCommand, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("cannot open file %s: %w", path, err)
	}
	defer func() {
		if cerr := file.Close(); cerr != nil {
			fmt.Printf("failed to close file %s: %v\n", path, cerr)
		}
	}()

	reader := bufio.NewReaderSize(file, 8*os.Getpagesize())

	var commands []JsonRpcCommand
	start := time.Now()
	if err := json.NewDecoder(reader).Decode(&commands); err != nil {
		return nil, fmt.Errorf("cannot parse JSON %s: %w", path, err)
	}
	metrics.UnmarshallingTime += time.Since(start)
	return commands, nil
}

// extractJsonCommands reads JSON-RPC commands from an archive file.
func extractJsonCommands(path string, sanitizeExt bool, metrics *TestMetrics) ([]JsonRpcCommand, error) {
	var commands []JsonRpcCommand
	err := archive.Extract(path, sanitizeExt, func(reader *tar.Reader) error {
		bufferedReader := bufio.NewReaderSize(reader, 8*os.Getpagesize())
		start := time.Now()
		if err := json.NewDecoder(bufferedReader).Decode(&commands); err != nil {
			return fmt.Errorf("failed to decode JSON: %w", err)
		}
		metrics.UnmarshallingTime += time.Since(start)
		return nil
	})
	if err != nil {
		return nil, errors.New("cannot extract archive file " + path)
	}
	return commands, nil
}
