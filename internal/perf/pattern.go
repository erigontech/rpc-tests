package perf

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// PatternInfo is the header of a JSONL pattern: its first line, {"pattern":{...}}. It is valid JSON, so a JSONL
// parser reads it, and it is not a JSON-RPC request, so rpc_perf skips it and copies it into its JSON report.
type PatternInfo struct {
	Description string `json:"description"`          // built from the generation parameters, or given with --description
	Network     string `json:"network,omitempty"`    // e.g. mainnet
	Generator   string `json:"generator,omitempty"`  // the command that wrote the pattern, without the node URL and the output file
	FirstBlock  uint64 `json:"firstBlock,omitempty"` // the oldest block the requests were built from
	LastBlock   uint64 `json:"lastBlock,omitempty"`  // the newest block the requests were built from
	Requests    int    `json:"requests,omitempty"`   // the requests of the pattern
	Created     string `json:"created,omitempty"`    // generation time, RFC 3339 UTC
}

// ParsePatternHeader returns the pattern info of a JSONL line, or false when the line is not a header:
// an object with a "pattern" object and no "method".
func ParsePatternHeader(line []byte) (*PatternInfo, bool) {
	line = bytes.TrimSpace(line)
	if !bytes.HasPrefix(line, []byte("{")) || !bytes.Contains(line, []byte(`"pattern"`)) {
		return nil, false
	}
	var h struct {
		Pattern *PatternInfo    `json:"pattern"`
		Method  json.RawMessage `json:"method"`
	}
	if json.Unmarshal(line, &h) != nil || h.Pattern == nil || h.Method != nil {
		return nil, false
	}
	return h.Pattern, true
}

// WritePatternHeader writes info as the header line of a JSONL pattern.
func WritePatternHeader(w io.Writer, info PatternInfo) error {
	line, err := json.Marshal(struct {
		Pattern PatternInfo `json:"pattern"`
	}{info})
	if err != nil {
		return err
	}
	_, err = w.Write(append(line, '\n'))
	return err
}

// ReadPatternHeader returns the header of a JSONL pattern, or nil when its first non-empty line is a request.
// The second result is the offset of the line after the header, where the requests start.
func ReadPatternHeader(path string) (*PatternInfo, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 64*1024)
	var offset int64
	for {
		line, err := r.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			info, ok := ParsePatternHeader(line)
			if !ok {
				return nil, 0, nil
			}
			return info, offset + int64(len(line)), nil
		}
		offset += int64(len(line))
		if err == io.EOF {
			return nil, 0, nil
		}
		if err != nil {
			return nil, 0, fmt.Errorf("%s: %w", path, err)
		}
	}
}

// requestsChecksum returns the BSD checksum of the requests of a JSONL pattern with a header, the part after
// the header, as the sum command prints it: the same as the checksum of the file without its header, so that
// editing the description does not change it.
func requestsChecksum(path string, offset int64) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return "", err
	}
	return bsdSum(f)
}

// bsdSum returns the 16-bit BSD checksum of r, the default algorithm of sum, formatted as sum prints it.
func bsdSum(r io.Reader) (string, error) {
	var sum uint32
	br := bufio.NewReaderSize(r, 64*1024)
	for {
		b, err := br.ReadByte()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		sum = (sum >> 1) + ((sum & 1) << 15)
		sum = (sum + uint32(b)) & 0xffff
	}
	return fmt.Sprintf("%05d", sum), nil
}
