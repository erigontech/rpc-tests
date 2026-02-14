package tools

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/urfave/cli/v2"
)

var graphqlCommand = &cli.Command{
	Name:  "graphql",
	Usage: "Execute GraphQL queries against an Ethereum node",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "http-url",
			Value: "http://127.0.0.1:8545/graphql",
			Usage: "GraphQL URL of the Ethereum node",
		},
		&cli.StringFlag{
			Name:  "query",
			Usage: "GraphQL query string (mutually exclusive with --tests-url)",
		},
		&cli.StringFlag{
			Name:  "tests-url",
			Usage: "GitHub tree URL with test files (mutually exclusive with --query)",
		},
		&cli.BoolFlag{
			Name:  "stop-at-first-error",
			Usage: "Stop execution at first test error",
		},
		&cli.IntFlag{
			Name:  "test-number",
			Value: -1,
			Usage: "Run only the test at this index (0-based)",
		},
	},
	Action: runGraphQL,
}

type graphqlTestCase struct {
	Request   string            `json:"request"`
	Responses []json.RawMessage `json:"responses"`
}

func runGraphQL(c *cli.Context) error {
	httpURL := c.String("http-url")
	query := c.String("query")
	testsURL := c.String("tests-url")
	stopAtError := c.Bool("stop-at-first-error")
	testNumber := c.Int("test-number")

	if query == "" && testsURL == "" {
		return fmt.Errorf("must specify either --query or --tests-url")
	}
	if query != "" && testsURL != "" {
		return fmt.Errorf("--query and --tests-url are mutually exclusive")
	}

	client := &http.Client{}

	if query != "" {
		result, err := executeGraphQLQuery(client, httpURL, query)
		if err != nil {
			return err
		}
		log.Printf("Result: %s", result)
		return nil
	}

	return executeGraphQLTests(client, httpURL, testsURL, stopAtError, testNumber)
}

func executeGraphQLQuery(client *http.Client, url, query string) ([]byte, error) {
	payload, err := json.Marshal(map[string]string{"query": query})
	if err != nil {
		return nil, fmt.Errorf("marshal query: %w", err)
	}
	req, err := http.NewRequest("POST", url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute query: %w", err)
	}
	defer resp.Body.Close()

	return io.ReadAll(resp.Body)
}

func executeGraphQLTests(client *http.Client, httpURL, testsURL string, stopAtError bool, testNumber int) error {
	// Download test files from GitHub
	tempDir, err := downloadGitHubDirectory(client, testsURL)
	if err != nil {
		return fmt.Errorf("download tests: %w", err)
	}
	defer func() {
		log.Printf("Cleaning up temporary directory: %s", tempDir)
		_ = os.RemoveAll(tempDir)
	}()

	log.Printf("Starting test execution using files from %s", tempDir)

	// Discover and sort test files
	entries, err := os.ReadDir(tempDir)
	if err != nil {
		return fmt.Errorf("read test dir: %w", err)
	}

	var testFiles []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			testFiles = append(testFiles, e.Name())
		}
	}
	sort.Strings(testFiles)

	if len(testFiles) == 0 {
		log.Printf("Warning: no *.json files found in %s. Aborting tests.", tempDir)
		return fmt.Errorf("no test files found")
	}

	totalTests := len(testFiles)
	if testNumber >= 0 {
		totalTests = 1
	}
	passedTests := 0

	graphqlClient := &http.Client{}

	for i, testFile := range testFiles {
		if testNumber >= 0 && testNumber != i {
			continue
		}

		testPath := filepath.Join(tempDir, testFile)
		data, err := os.ReadFile(testPath)
		if err != nil {
			log.Printf("Test %d FAILED: cannot read %s: %v", i+1, testFile, err)
			continue
		}

		var tc graphqlTestCase
		if err := json.Unmarshal(data, &tc); err != nil {
			log.Printf("Test %d FAILED: invalid JSON in %s: %v", i+1, testFile, err)
			continue
		}

		if tc.Request == "" {
			log.Printf("Test %d FAILED: 'request' field is missing in %s", i+1, testFile)
			continue
		}
		if len(tc.Responses) == 0 {
			log.Printf("Test %d FAILED: 'responses' field is missing in %s", i+1, testFile)
			continue
		}

		// Execute query
		actualResult, err := executeGraphQLQuery(graphqlClient, httpURL, strings.TrimSpace(tc.Request))
		if err != nil {
			log.Printf("Test %d FAILED: query execution error: %v", i+1, err)
			if stopAtError {
				log.Printf("Testing finished after first error. Passed: %d/%d", passedTests, totalTests)
				return fmt.Errorf("stopped at first error")
			}
			continue
		}

		// Parse actual result
		var actualData map[string]any
		if err := json.Unmarshal(actualResult, &actualData); err != nil {
			log.Printf("Test %d FAILED: cannot parse response: %v", i+1, err)
			continue
		}

		// Compare actual vs expected: test passes if actual matches ANY expected response
		passing := false
		for _, expectedRaw := range tc.Responses {
			var expected map[string]any
			if err := json.Unmarshal(expectedRaw, &expected); err != nil {
				continue
			}

			// Check if actual data matches expected data
			actualDataField := actualData["data"]
			expectedDataField := expected["data"]
			if jsonEqual(actualDataField, expectedDataField) {
				passing = true
				break
			}

			// Check if both have errors
			if expected["errors"] != nil && actualData["errors"] != nil {
				passing = true
				break
			}
		}

		if passing {
			passedTests++
			log.Printf("Test %d %s PASSED.", i+1, testFile)
		} else {
			log.Printf("Test %d %s FAILED: actual result didn't match any expected response.", i+1, testFile)
			log.Printf("Request: %s", strings.TrimSpace(tc.Request))
			log.Printf("Actual:  %s", string(actualResult))
			if stopAtError {
				log.Printf("Testing finished after first error. Passed: %d/%d", passedTests, totalTests)
				return fmt.Errorf("stopped at first error")
			}
		}
	}

	log.Printf("Testing finished. Passed: %d/%d", passedTests, totalTests)
	if passedTests != totalTests {
		return fmt.Errorf("some tests failed: %d/%d passed", passedTests, totalTests)
	}
	return nil
}

func jsonEqual(a, b any) bool {
	aj, err1 := json.Marshal(a)
	bj, err2 := json.Marshal(b)
	if err1 != nil || err2 != nil {
		return false
	}
	return bytes.Equal(aj, bj)
}

type githubContent struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	DownloadURL string `json:"download_url"`
}

func parseGitHubTreeURL(rawURL string) (owner, repo, branch, folderPath string, err error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", "", "", "", fmt.Errorf("parse URL: %w", err)
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 5 || parts[2] != "tree" {
		return "", "", "", "", fmt.Errorf("invalid GitHub tree URL format: %s", rawURL)
	}
	owner = parts[0]
	repo = parts[1]
	branch = parts[3]
	folderPath = strings.Join(parts[4:], "/")
	return
}

func downloadGitHubDirectory(client *http.Client, treeURL string) (string, error) {
	owner, repo, branch, folderPath, err := parseGitHubTreeURL(treeURL)
	if err != nil {
		return "", err
	}

	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/%s?ref=%s", owner, repo, folderPath, branch)

	tempDir, err := os.MkdirTemp("", "graphql-tests-*")
	if err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}
	log.Printf("Downloading test files to temporary directory: %s", tempDir)

	resp, err := client.Get(apiURL)
	if err != nil {
		_ = os.RemoveAll(tempDir)
		return "", fmt.Errorf("fetch GitHub API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		_ = os.RemoveAll(tempDir)
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("GitHub API error %d: %s", resp.StatusCode, string(body[:min(len(body), 100)]))
	}

	var contents []githubContent
	if err := json.NewDecoder(resp.Body).Decode(&contents); err != nil {
		_ = os.RemoveAll(tempDir)
		return "", fmt.Errorf("decode GitHub API response: %w", err)
	}

	downloaded := 0
	for _, item := range contents {
		if item.Type != "file" || !strings.HasSuffix(item.Name, ".json") {
			continue
		}

		fileResp, err := client.Get(item.DownloadURL)
		if err != nil {
			log.Printf("Warning: failed to download %s: %v", item.Name, err)
			continue
		}

		data, err := io.ReadAll(fileResp.Body)
		fileResp.Body.Close()
		if err != nil {
			log.Printf("Warning: failed to read %s: %v", item.Name, err)
			continue
		}

		if err := os.WriteFile(filepath.Join(tempDir, item.Name), data, 0644); err != nil {
			log.Printf("Warning: failed to write %s: %v", item.Name, err)
			continue
		}
		downloaded++
	}

	log.Printf("Downloaded %d test files.", downloaded)
	return tempDir, nil
}
