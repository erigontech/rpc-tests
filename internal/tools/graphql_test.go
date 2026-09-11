package tools

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseGitHubTreeURL(t *testing.T) {
	tests := []struct {
		name       string
		rawURL     string
		owner      string
		repo       string
		branch     string
		folderPath string
	}{
		{
			name:       "single folder segment",
			rawURL:     "https://github.com/erigontech/rpc-tests/tree/main/graphql",
			owner:      "erigontech",
			repo:       "rpc-tests",
			branch:     "main",
			folderPath: "graphql",
		},
		{
			name:       "nested folder segments",
			rawURL:     "https://github.com/erigontech/rpc-tests/tree/main/integration/mainnet/graphql",
			owner:      "erigontech",
			repo:       "rpc-tests",
			branch:     "main",
			folderPath: "integration/mainnet/graphql",
		},
		{
			name:       "trailing slash",
			rawURL:     "https://github.com/o/r/tree/dev/tests/",
			owner:      "o",
			repo:       "r",
			branch:     "dev",
			folderPath: "tests",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, branch, folderPath, err := parseGitHubTreeURL(tt.rawURL)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if owner != tt.owner || repo != tt.repo || branch != tt.branch || folderPath != tt.folderPath {
				t.Errorf("got (%q, %q, %q, %q), want (%q, %q, %q, %q)",
					owner, repo, branch, folderPath, tt.owner, tt.repo, tt.branch, tt.folderPath)
			}
		})
	}
}

func TestParseGitHubTreeURLInvalid(t *testing.T) {
	tests := []struct {
		name   string
		rawURL string
	}{
		{"too few segments", "https://github.com/erigontech/rpc-tests"},
		{"blob instead of tree", "https://github.com/o/r/blob/main/tests"},
		{"no folder path", "https://github.com/o/r/tree/main"},
		{"empty", ""},
		{"unparseable", "https://github.com/o/r/tree/main/x\x7f"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, _, _, err := parseGitHubTreeURL(tt.rawURL); err == nil {
				t.Errorf("expected an error for %q", tt.rawURL)
			}
		})
	}
}

func TestJSONEqual(t *testing.T) {
	tests := []struct {
		name string
		a, b any
		want bool
	}{
		{"identical maps", map[string]any{"a": 1.0}, map[string]any{"a": 1.0}, true},
		{"different values", map[string]any{"a": 1.0}, map[string]any{"a": 2.0}, false},
		{"both nil", nil, nil, true},
		{"nil vs empty map", nil, map[string]any{}, false},
		{"nested equal", map[string]any{"b": []any{1.0, "x"}}, map[string]any{"b": []any{1.0, "x"}}, true},
		{"array order matters", []any{1.0, 2.0}, []any{2.0, 1.0}, false},
		{"unmarshalable operand", make(chan int), make(chan int), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := jsonEqual(tt.a, tt.b); got != tt.want {
				t.Errorf("jsonEqual(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

// TestJSONEqualIgnoresMapOrder pins that Go's canonical (key-sorted) marshalling
// makes the comparison independent of literal key order.
func TestJSONEqualIgnoresMapOrder(t *testing.T) {
	a := map[string]any{"z": 1.0, "a": 2.0}
	b := map[string]any{"a": 2.0, "z": 1.0}
	if !jsonEqual(a, b) {
		t.Error("maps with the same entries should compare equal")
	}
}

func TestExecuteGraphQLQuery(t *testing.T) {
	var gotBody map[string]string
	var gotContentType string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = w.Write([]byte(`{"data":{"block":{"number":42}}}`))
	}))
	defer server.Close()

	result, err := executeGraphQLQuery(server.Client(), server.URL, "{block{number}}")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type: got %q, want application/json", gotContentType)
	}
	if gotBody["query"] != "{block{number}}" {
		t.Errorf("query body: got %q", gotBody["query"])
	}
	if string(result) != `{"data":{"block":{"number":42}}}` {
		t.Errorf("result: got %s", result)
	}
}

func TestExecuteGraphQLQueryTransportError(t *testing.T) {
	if _, err := executeGraphQLQuery(http.DefaultClient, "http://"+unusedPort, "{x}"); err == nil {
		t.Error("expected an error when the endpoint is unreachable")
	}
}

func TestExecuteGraphQLQueryBadURL(t *testing.T) {
	if _, err := executeGraphQLQuery(http.DefaultClient, "://not a url", "{x}"); err == nil {
		t.Error("expected an error for a malformed URL")
	}
}

func TestRunGraphQLRequiresOneSource(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"neither flag", []string{"graphql"}, "must specify either"},
		{
			name: "both flags",
			args: []string{"graphql", "--query", "{x}", "--tests-url", "https://github.com/o/r/tree/main/t"},
			want: "mutually exclusive",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := runSubcommand(tt.args...)
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not mention %q", err, tt.want)
			}
		})
	}
}

func TestRunGraphQLSingleQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"block":{"number":"0x1"}}}`))
	}))
	defer server.Close()

	if err := runSubcommand("graphql", "--http-url", server.URL, "--query", "{block{number}}"); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunGraphQLInvalidTestsURL(t *testing.T) {
	err := runSubcommand("graphql", "--tests-url", "https://github.com/not-a-tree-url")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "download tests") {
		t.Errorf("error %q should be wrapped as a download failure", err)
	}
}

// graphqlTestDir writes GraphQL fixture files and returns the directory.
func graphqlTestDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return dir
}

func TestExecuteGraphQLTestsAllPass(t *testing.T) {
	dir := graphqlTestDir(t, map[string]string{
		"test_01.json": `{"request":"{block{number}}","responses":[{"data":{"block":{"number":1}}}]}`,
		"test_02.json": `{"request":"{block{hash}}","responses":[{"data":{"block":{"hash":"0xaa"}}}]}`,
	})

	responses := map[string]string{
		"{block{number}}": `{"data":{"block":{"number":1}}}`,
		"{block{hash}}":   `{"data":{"block":{"hash":"0xaa"}}}`,
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = io.WriteString(w, responses[body["query"]])
	}))
	defer server.Close()

	if err := runGraphQLTestsInDir(server.Client(), server.URL, dir, false, -1); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestExecuteGraphQLTestsMismatchFails(t *testing.T) {
	dir := graphqlTestDir(t, map[string]string{
		"test_01.json": `{"request":"{block{number}}","responses":[{"data":{"block":{"number":1}}}]}`,
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"data":{"block":{"number":999}}}`)
	}))
	defer server.Close()

	if err := runGraphQLTestsInDir(server.Client(), server.URL, dir, false, -1); err == nil {
		t.Error("expected a failure when the response does not match")
	}
}

// TestExecuteGraphQLTestsMatchesAnyExpected covers the "passes if it matches at
// least one of the listed responses" rule.
func TestExecuteGraphQLTestsMatchesAnyExpected(t *testing.T) {
	dir := graphqlTestDir(t, map[string]string{
		"test_01.json": `{"request":"{block{number}}","responses":[` +
			`{"data":{"block":{"number":1}}},{"data":{"block":{"number":2}}}]}`,
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"data":{"block":{"number":2}}}`)
	}))
	defer server.Close()

	if err := runGraphQLTestsInDir(server.Client(), server.URL, dir, false, -1); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestExecuteGraphQLTestsErrorsMatch covers the rule that a test passes when
// both the expectation and the actual response carry an "errors" field.
func TestExecuteGraphQLTestsErrorsMatch(t *testing.T) {
	dir := graphqlTestDir(t, map[string]string{
		"test_01.json": `{"request":"{bogus}","responses":[{"errors":[{"message":"whatever"}]}]}`,
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"errors":[{"message":"unknown field"}]}`)
	}))
	defer server.Close()

	if err := runGraphQLTestsInDir(server.Client(), server.URL, dir, false, -1); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestExecuteGraphQLTestsSelectsSingleTest(t *testing.T) {
	dir := graphqlTestDir(t, map[string]string{
		"test_01.json": `{"request":"{first}","responses":[{"data":{"v":1}}]}`,
		"test_02.json": `{"request":"{second}","responses":[{"data":{"v":2}}]}`,
		"test_03.json": `{"request":"{third}","responses":[{"data":{"v":3}}]}`,
	})

	var queries []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		queries = append(queries, body["query"])
		_, _ = io.WriteString(w, `{"data":{"v":2}}`)
	}))
	defer server.Close()

	if err := runGraphQLTestsInDir(server.Client(), server.URL, dir, false, 1); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(queries) != 1 || queries[0] != "{second}" {
		t.Errorf("expected only test index 1 to run, got %v", queries)
	}
}

func TestExecuteGraphQLTestsStopsAtFirstError(t *testing.T) {
	dir := graphqlTestDir(t, map[string]string{
		"test_01.json": `{"request":"{a}","responses":[{"data":{"v":1}}]}`,
		"test_02.json": `{"request":"{b}","responses":[{"data":{"v":2}}]}`,
	})

	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = io.WriteString(w, `{"data":{"v":"unexpected"}}`)
	}))
	defer server.Close()

	err := runGraphQLTestsInDir(server.Client(), server.URL, dir, true, -1)
	if err == nil {
		t.Fatal("expected an error")
	}
	if calls != 1 {
		t.Errorf("expected to stop after the first test, got %d calls", calls)
	}
}

func TestExecuteGraphQLTestsSkipsMalformedFixtures(t *testing.T) {
	dir := graphqlTestDir(t, map[string]string{
		"test_01.json": `not json at all`,
		"test_02.json": `{"responses":[{"data":{}}]}`, // missing request
		"test_03.json": `{"request":"{a}"}`,           // missing responses
		"ignored.txt":  `{"request":"{a}","responses":[{"data":{}}]}`,
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("no query should reach the server")
	}))
	defer server.Close()

	// All three fixtures are skipped, so no test passes and the run fails.
	if err := runGraphQLTestsInDir(server.Client(), server.URL, dir, false, -1); err == nil {
		t.Error("expected an error when every fixture is malformed")
	}
}

func TestExecuteGraphQLTestsNoFixtures(t *testing.T) {
	dir := graphqlTestDir(t, nil)
	err := runGraphQLTestsInDir(http.DefaultClient, "http://example.invalid", dir, false, -1)
	if err == nil || !strings.Contains(err.Error(), "no test files") {
		t.Errorf("got %v, want a 'no test files' error", err)
	}
}

func TestDownloadGitHubDirectory(t *testing.T) {
	var fileServer *httptest.Server
	fileServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api":
			_, _ = io.WriteString(w, `[
				{"name":"test_01.json","type":"file","download_url":"`+fileServer.URL+`/test_01.json"},
				{"name":"README.md","type":"file","download_url":"`+fileServer.URL+`/README.md"},
				{"name":"subdir","type":"dir","download_url":""}
			]`)
		case "/test_01.json":
			_, _ = io.WriteString(w, `{"request":"{a}","responses":[]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer fileServer.Close()

	dir, err := downloadContents(fileServer.Client(), fileServer.URL+"/api")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer os.RemoveAll(dir)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "test_01.json" {
		t.Errorf("expected only test_01.json to be downloaded, got %v", entries)
	}
}

func TestDownloadContentsAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limit exceeded", http.StatusForbidden)
	}))
	defer server.Close()

	if _, err := downloadContents(server.Client(), server.URL); err == nil {
		t.Error("expected an error for a non-200 GitHub API response")
	}
}

func TestDownloadContentsMalformedJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"not":"an array"}`)
	}))
	defer server.Close()

	if _, err := downloadContents(server.Client(), server.URL); err == nil {
		t.Error("expected an error when the API response cannot be decoded")
	}
}

func TestDownloadContentsUnreachable(t *testing.T) {
	if _, err := downloadContents(http.DefaultClient, "http://"+unusedPort); err == nil {
		t.Error("expected an error when the API is unreachable")
	}
}
