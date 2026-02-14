package tools

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/urfave/cli/v2"
)

var replayRequestCommand = &cli.Command{
	Name:  "replay-request",
	Usage: "Replay JSON-RPC requests from Engine API log files",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "method",
			Value: "engine_newPayloadV3",
			Usage: "JSON-RPC method to replay",
		},
		&cli.IntFlag{
			Name:  "index",
			Value: 1,
			Usage: "Ordinal index of method occurrence to replay (-1 for all)",
		},
		&cli.StringFlag{
			Name:  "jwt",
			Usage: "Path to JWT secret file (default: $HOME/prysm/jwt.hex)",
		},
		&cli.StringFlag{
			Name:  "path",
			Usage: "Path to Engine API log directory (default: platform-specific Silkworm/logs)",
		},
		&cli.StringFlag{
			Name:  "url",
			Value: "http://localhost:8551",
			Usage: "HTTP URL of Engine API endpoint",
		},
		&cli.BoolFlag{
			Name:  "pretend",
			Usage: "Do not send any HTTP request, just pretend",
		},
		&cli.BoolFlag{
			Name:    "verbose",
			Aliases: []string{"v"},
			Usage:   "Print verbose output",
		},
	},
	Action: runReplayRequest,
}

func runReplayRequest(c *cli.Context) error {
	method := c.String("method")
	methodIndex := c.Int("index")
	jwtFile := c.String("jwt")
	logPath := c.String("path")
	targetURL := c.String("url")
	pretend := c.Bool("pretend")
	verbose := c.Bool("verbose")

	// Default JWT file
	if jwtFile == "" {
		home, _ := os.UserHomeDir()
		jwtFile = filepath.Join(home, "prysm", "jwt.hex")
	}

	// Default log path
	if logPath == "" {
		logPath = getDefaultLogPath()
	}

	// Build headers
	headers := map[string]string{
		"Content-Type": "application/json",
	}

	// Read JWT and create auth token
	jwtAuth, err := encodeJWTToken(jwtFile)
	if err != nil {
		log.Printf("Warning: JWT auth not available: %v", err)
	} else {
		headers["Authorization"] = "Bearer " + jwtAuth
	}

	// Find the request
	request, err := findJSONRPCRequest(logPath, method, methodIndex, verbose)
	if err != nil {
		return err
	}
	if request == "" {
		log.Printf("Request %s not found [%d]", method, methodIndex)
		return nil
	}

	log.Printf("Request %s found [%d]", method, methodIndex)
	if verbose {
		log.Printf("%s", request)
	}

	if pretend {
		return nil
	}

	// Send HTTP request
	req, err := http.NewRequest("POST", targetURL, bytes.NewBufferString(request))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	client := &http.Client{Timeout: 300 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("post failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	log.Printf("Response got: %s", string(body))

	return nil
}

func getDefaultLogPath() string {
	home, _ := os.UserHomeDir()
	// Darwin: ~/Library/Silkworm/logs
	// Linux: ~/Silkworm/logs
	if _, err := os.Stat(filepath.Join(home, "Library")); err == nil {
		return filepath.Join(home, "Library", "Silkworm", "logs")
	}
	return filepath.Join(home, "Silkworm", "logs")
}

func encodeJWTToken(jwtFile string) (string, error) {
	data, err := os.ReadFile(jwtFile)
	if err != nil {
		return "", err
	}
	contents := strings.TrimPrefix(strings.TrimSpace(string(data)), "0x")

	secretBytes, err := hex.DecodeString(contents)
	if err != nil {
		return "", fmt.Errorf("decode JWT secret: %w", err)
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"iat": time.Now().Unix(),
	})
	return token.SignedString(secretBytes)
}

func findJSONRPCRequest(logDir, method string, methodIndex int, verbose bool) (string, error) {
	// Find all engine_rpc_api log files
	pattern := filepath.Join(logDir, "*engine_rpc_api*log")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return "", fmt.Errorf("glob log files: %w", err)
	}
	if len(matches) == 0 {
		// Try alternative: the path itself might be a file
		if _, err := os.Stat(logDir); err == nil {
			matches = []string{logDir}
		} else {
			return "", fmt.Errorf("no engine_rpc_api log files found in %s", logDir)
		}
	}
	sort.Strings(matches)

	if verbose {
		log.Printf("interface_log_dir_path: %s", logDir)
	}

	methodCount := 0
	for _, logFile := range matches {
		if verbose {
			log.Printf("log_file_path: %s", logFile)
		}

		data, err := os.ReadFile(logFile)
		if err != nil {
			log.Printf("Warning: cannot read %s: %v", logFile, err)
			continue
		}

		for _, line := range strings.Split(string(data), "\n") {
			reqIdx := strings.Index(line, "REQ -> ")
			if reqIdx == -1 {
				continue
			}

			if verbose {
				methodPos := strings.Index(line, "method")
				if methodPos != -1 {
					end := min(methodPos+40, len(line))
					log.Printf("Method %s found %s", line[methodPos:end], logFile)
				}
			}

			if !strings.Contains(line, method) {
				continue
			}

			methodCount++
			if methodCount == methodIndex {
				return line[reqIdx+len("REQ -> "):], nil
			}
		}
	}

	return "", nil
}
