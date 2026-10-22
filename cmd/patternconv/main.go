package main

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"strings"

	"github.com/dsnet/compress/bzip2"
	"github.com/urfave/cli/v2"
)

func main() {
	if err := newApp().Run(os.Args); err != nil {
		log.Fatal(err)
	}
}

func newApp() *cli.App {
	return &cli.App{
		Name:  "rpc_pattern_convert",
		Usage: "Convert a Vegeta pattern (a tar or a vegeta_*.txt file) to JSONL, one JSON-RPC request per line",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "in", Required: true, Usage: "Vegeta pattern: a tar (.tar, .tar.gz, .tgz, .tar.bz2) holding vegeta_erigon_<name>.txt, or a vegeta target file"},
			&cli.StringFlag{Name: "out", Required: true, Usage: "Output JSONL file, written only when the whole pattern converts"},
			&cli.StringFlag{Name: "name", Usage: "With a tar holding several non-empty vegeta_erigon_<name>.txt files, the one to convert"},
		},
		Action: func(c *cli.Context) error {
			var out bytes.Buffer
			n, err := convert(c.String("in"), c.String("name"), &out)
			if err != nil {
				return err
			}
			if err := os.WriteFile(c.String("out"), out.Bytes(), 0o644); err != nil {
				return err
			}
			fmt.Printf("%d requests written to %s\n", n, c.String("out"))
			return nil
		},
	}
}

// convert writes the requests of the vegeta pattern in to out, one compacted JSON-RPC request per line,
// and returns their number.
func convert(in, name string, out io.Writer) (int, error) {
	if !strings.Contains(path.Base(in), ".tar") && !strings.HasSuffix(in, ".tgz") {
		f, err := os.Open(in)
		if err != nil {
			return 0, err
		}
		defer f.Close()
		return convertTargets(f, in, out)
	}
	content, entry, err := readPattern(in, name)
	if err != nil {
		return 0, err
	}
	return convertTargets(bytes.NewReader(content), in+":"+entry, out)
}

// readPattern returns the content and the name of the vegeta_erigon_<name>.txt file of a tar. The empty
// ones are skipped; with several non-empty ones, name selects the file.
func readPattern(in, name string) ([]byte, string, error) {
	f, err := os.Open(in)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()
	// The compression is read from the content: the historical patterns are gzip or bzip2 tars named .tar.
	br := bufio.NewReader(f)
	magic, _ := br.Peek(3)
	var r io.Reader = br
	switch {
	case bytes.HasPrefix(magic, []byte{0x1f, 0x8b}):
		gz, err := gzip.NewReader(br)
		if err != nil {
			return nil, "", err
		}
		defer gz.Close()
		r = gz
	case bytes.HasPrefix(magic, []byte("BZh")):
		bz, err := bzip2.NewReader(br, nil)
		if err != nil {
			return nil, "", err
		}
		defer bz.Close()
		r = bz
	}

	patterns := map[string][]byte{}
	var names []string
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, "", fmt.Errorf("%s: %w", in, err)
		}
		base := path.Base(hdr.Name)
		pattern, ok := strings.CutPrefix(base, "vegeta_erigon_")
		if hdr.Typeflag != tar.TypeReg || hdr.Size == 0 || !ok || !strings.HasSuffix(pattern, ".txt") {
			continue
		}
		pattern = strings.TrimSuffix(pattern, ".txt")
		if name != "" && pattern != name {
			continue
		}
		content, err := io.ReadAll(tr)
		if err != nil {
			return nil, "", fmt.Errorf("%s: %w", in, err)
		}
		patterns[pattern] = content
		names = append(names, pattern)
	}
	switch {
	case len(names) == 0 && name != "":
		return nil, "", fmt.Errorf("%s: no non-empty vegeta_erigon_%s.txt", in, name)
	case len(names) == 0:
		return nil, "", fmt.Errorf("%s: no non-empty vegeta_erigon_*.txt", in)
	case len(names) > 1:
		return nil, "", fmt.Errorf("%s holds several patterns (%s): select one with --name", in, strings.Join(names, ", "))
	}
	return patterns[names[0]], "vegeta_erigon_" + names[0] + ".txt", nil
}

// convertTargets writes the body of each vegeta target of r to out, compacted, one per line.
func convertTargets(r io.Reader, source string, out io.Writer) (int, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 1<<20), 64<<20)
	var line bytes.Buffer
	n := 0
	for i := 1; sc.Scan(); i++ {
		if len(sc.Bytes()) == 0 {
			continue
		}
		var target struct {
			Body []byte `json:"body"` // base64 in the file, decoded by encoding/json
		}
		if err := json.Unmarshal(sc.Bytes(), &target); err != nil {
			return n, fmt.Errorf("%s line %d: not a vegeta target: %w", source, i, err)
		}
		line.Reset()
		if err := json.Compact(&line, target.Body); err != nil {
			return n, fmt.Errorf("%s line %d: body is not JSON: %w", source, i, err)
		}
		line.WriteByte('\n')
		if _, err := out.Write(line.Bytes()); err != nil {
			return n, err
		}
		n++
	}
	return n, sc.Err()
}
