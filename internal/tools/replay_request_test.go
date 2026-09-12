package tools

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

func writeJWTFile(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "jwt.hex")
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatalf("write jwt file: %v", err)
	}
	return path
}

func TestEncodeJWTToken(t *testing.T) {
	secret := strings.Repeat("ab", 32)
	tests := []struct {
		name     string
		contents string
	}{
		{"plain hex", secret},
		{"0x prefixed", "0x" + secret},
		{"surrounded by whitespace", "  \n" + secret + "\n  "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token, err := encodeJWTToken(writeJWTFile(t, tt.contents))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			secretBytes, err := hex.DecodeString(secret)
			if err != nil {
				t.Fatalf("decode secret: %v", err)
			}
			parsed, err := jwt.Parse(token, func(*jwt.Token) (any, error) { return secretBytes, nil })
			if err != nil {
				t.Fatalf("token does not verify against the secret: %v", err)
			}
			claims, ok := parsed.Claims.(jwt.MapClaims)
			if !ok {
				t.Fatalf("unexpected claims type %T", parsed.Claims)
			}
			if _, ok := claims["iat"]; !ok {
				t.Error("token is missing the required iat claim")
			}
			if parsed.Method.Alg() != "HS256" {
				t.Errorf("alg: got %s, want HS256", parsed.Method.Alg())
			}
		})
	}
}

func TestEncodeJWTTokenMissingFile(t *testing.T) {
	if _, err := encodeJWTToken(filepath.Join(t.TempDir(), "absent.hex")); err == nil {
		t.Error("expected an error for a missing secret file")
	}
}

func TestEncodeJWTTokenInvalidHex(t *testing.T) {
	if _, err := encodeJWTToken(writeJWTFile(t, "not-hex-at-all")); err == nil {
		t.Error("expected an error for a non-hex secret")
	}
}

func TestGetDefaultLogPath(t *testing.T) {
	path := getDefaultLogPath()
	if path == "" {
		t.Fatal("default log path should never be empty")
	}
	if !strings.HasSuffix(path, filepath.Join("Silkworm", "logs")) {
		t.Errorf("path %q should end in Silkworm/logs", path)
	}
}

// writeEngineLog creates a log directory holding engine_rpc_api log files.
func writeEngineLog(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return dir
}

func TestFindJSONRPCRequest(t *testing.T) {
	const (
		first  = `{"jsonrpc":"2.0","method":"engine_newPayloadV3","params":["first"],"id":1}`
		second = `{"jsonrpc":"2.0","method":"engine_newPayloadV3","params":["second"],"id":2}`
		other  = `{"jsonrpc":"2.0","method":"engine_forkchoiceUpdatedV3","params":[],"id":3}`
	)
	dir := writeEngineLog(t, map[string]string{
		"engine_rpc_api.log": strings.Join([]string{
			"[INFO] starting up",
			"[TRACE] REQ -> " + first,
			"[TRACE] RSP <- {}",
			"[TRACE] REQ -> " + other,
			"[TRACE] REQ -> " + second,
		}, "\n"),
	})

	tests := []struct {
		name  string
		index int
		want  string
	}{
		{"first occurrence", 1, first},
		{"second occurrence", 2, second},
		{"beyond the last occurrence", 3, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := findJSONRPCRequest(dir, "engine_newPayloadV3", tt.index, false)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// TestFindJSONRPCRequestSpansSortedFiles checks that occurrences are counted
// across every matching log file, in sorted filename order.
func TestFindJSONRPCRequestSpansSortedFiles(t *testing.T) {
	dir := writeEngineLog(t, map[string]string{
		"2-engine_rpc_api.log": "REQ -> second-file engine_newPayloadV3",
		"1-engine_rpc_api.log": "REQ -> first-file engine_newPayloadV3",
	})

	got, err := findJSONRPCRequest(dir, "engine_newPayloadV3", 2, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "second-file engine_newPayloadV3" {
		t.Errorf("got %q, want the occurrence from the second file", got)
	}
}

func TestFindJSONRPCRequestAcceptsSingleFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "captured.txt")
	body := `{"jsonrpc":"2.0","method":"engine_newPayloadV3","id":1}`
	if err := os.WriteFile(path, []byte("REQ -> "+body), 0644); err != nil {
		t.Fatalf("write log: %v", err)
	}

	got, err := findJSONRPCRequest(path, "engine_newPayloadV3", 1, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != body {
		t.Errorf("got %q, want %q", got, body)
	}
}

func TestFindJSONRPCRequestNoLogFiles(t *testing.T) {
	_, err := findJSONRPCRequest(filepath.Join(t.TempDir(), "absent"), "engine_newPayloadV3", 1, false)
	if err == nil {
		t.Fatal("expected an error when no log files exist")
	}
	if !strings.Contains(err.Error(), "no engine_rpc_api log files") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestFindJSONRPCRequestIgnoresLinesWithoutMarker(t *testing.T) {
	dir := writeEngineLog(t, map[string]string{
		"engine_rpc_api.log": "engine_newPayloadV3 mentioned without the REQ marker",
	})
	got, err := findJSONRPCRequest(dir, "engine_newPayloadV3", 1, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestRunReplayRequestPretendDoesNotDial(t *testing.T) {
	body := `{"jsonrpc":"2.0","method":"engine_newPayloadV3","id":1}`
	dir := writeEngineLog(t, map[string]string{"engine_rpc_api.log": "REQ -> " + body})

	// --url points at a closed port: with --pretend nothing must be sent.
	err := runSubcommand("replay-request",
		"--path", dir,
		"--jwt", writeJWTFile(t, strings.Repeat("cd", 32)),
		"--url", "http://"+unusedPort,
		"--pretend",
		"--verbose",
	)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunReplayRequestNotFoundIsNotAnError(t *testing.T) {
	dir := writeEngineLog(t, map[string]string{"engine_rpc_api.log": "REQ -> something else"})
	err := runSubcommand("replay-request", "--path", dir, "--method", "engine_newPayloadV3")
	if err != nil {
		t.Errorf("a missing request should be reported, not returned as an error: %v", err)
	}
}

func TestRunReplayRequestPostsToEndpoint(t *testing.T) {
	body := `{"jsonrpc":"2.0","method":"engine_newPayloadV3","id":1}`
	dir := writeEngineLog(t, map[string]string{"engine_rpc_api.log": "REQ -> " + body})

	node := newFakeNode(t, map[string]func([]any) (any, *rpcErrorObject){
		"engine_newPayloadV3": func([]any) (any, *rpcErrorObject) {
			return map[string]any{"status": "VALID"}, nil
		},
	})

	err := runSubcommand("replay-request",
		"--path", dir,
		"--jwt", writeJWTFile(t, strings.Repeat("ef", 32)),
		"--url", node.url(),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if node.callsTo("engine_newPayloadV3") != 1 {
		t.Errorf("expected exactly one replayed request, got %d", node.callsTo("engine_newPayloadV3"))
	}
}

// TestRunReplayRequestContinuesWithoutJWT documents that an unreadable secret
// file only downgrades the request to unauthenticated instead of failing.
func TestRunReplayRequestContinuesWithoutJWT(t *testing.T) {
	body := `{"jsonrpc":"2.0","method":"engine_newPayloadV3","id":1}`
	dir := writeEngineLog(t, map[string]string{"engine_rpc_api.log": "REQ -> " + body})

	node := newFakeNode(t, map[string]func([]any) (any, *rpcErrorObject){
		"engine_newPayloadV3": func([]any) (any, *rpcErrorObject) { return "ok", nil },
	})

	err := runSubcommand("replay-request",
		"--path", dir,
		"--jwt", filepath.Join(t.TempDir(), "absent.hex"),
		"--url", node.url(),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if node.callsTo("engine_newPayloadV3") != 1 {
		t.Error("the request should still be sent without an Authorization header")
	}
}

func TestRunReplayRequestPostFailure(t *testing.T) {
	body := `{"jsonrpc":"2.0","method":"engine_newPayloadV3","id":1}`
	dir := writeEngineLog(t, map[string]string{"engine_rpc_api.log": "REQ -> " + body})

	err := runSubcommand("replay-request", "--path", dir, "--url", "http://"+unusedPort)
	if err == nil {
		t.Fatal("expected an error when the endpoint is unreachable")
	}
	if !strings.Contains(err.Error(), "post failed") {
		t.Errorf("unexpected error: %v", err)
	}
}
