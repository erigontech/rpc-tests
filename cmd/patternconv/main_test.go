package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dsnet/compress/bzip2"
)

const (
	call1 = `{"jsonrpc":"2.0","method":"eth_call","params":[{"to":"0x02"},"0x10"],"id":1}`
	call2 = `{"jsonrpc":"2.0","method":"eth_call","params":[{"to":"0x03"},"0x11"],"id":2}`
)

// vegetaFile returns vegeta targets posting bodies, written with spaces as the old generators did.
func vegetaFile(bodies ...string) string {
	var b strings.Builder
	for _, body := range bodies {
		spaced := strings.ReplaceAll(body, `,"`, `, "`)
		fmt.Fprintf(&b, `{"method": "POST", "url": "http://localhost:8545", "body": "%s", "header": {"Content-Type": ["application/json"]}}`+"\n",
			base64.StdEncoding.EncodeToString([]byte(spaced)))
	}
	return b.String()
}

// patternTar writes a tar laid out like the historical patterns, the directory first, compressed with
// compression ("", "gzip" or "bzip2") whatever the name.
func patternTar(t *testing.T, name, compression string, files map[string]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var w io.Writer = f
	var zw io.WriteCloser
	switch compression {
	case "gzip":
		zw = gzip.NewWriter(f)
	case "bzip2":
		if zw, err = bzip2.NewWriter(f, nil); err != nil {
			t.Fatal(err)
		}
	}
	if zw != nil {
		w = zw
	}
	tw := tar.NewWriter(w)
	if err := tw.WriteHeader(&tar.Header{Name: "erigon_stress_test/", Typeflag: tar.TypeDir, Mode: 0o755}); err != nil {
		t.Fatal(err)
	}
	for _, n := range slices.Sorted(maps.Keys(files)) {
		if err := tw.WriteHeader(&tar.Header{Name: "erigon_stress_test/" + n, Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(files[n]))}); err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(tw, files[n]); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if zw != nil {
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func convertToString(t *testing.T, in, name string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	_, err := convert(in, name, &out)
	return out.String(), err
}

func TestConvertTarWritesTheErigonRequestsOnePerLine(t *testing.T) {
	in := patternTar(t, "p.tar", "", map[string]string{
		"vegeta_erigon_eth_call.txt":   vegetaFile(call1, call2),
		"vegeta_geth_eth_call.txt":     vegetaFile(call2),
		"results_erigon_eth_call.csv":  "1, eth_call, 100\n",
		"vegeta_erigon_debug_noop.txt": "",
	})
	got, err := convertToString(t, in, "")
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if want := call1 + "\n" + call2 + "\n"; got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestConvertCompressedTarWhateverItsName(t *testing.T) {
	for _, tc := range []struct{ name, compression string }{
		{"p.tar.gz", "gzip"},
		{"p.tar", "gzip"},
		{"p.tar", "bzip2"},
	} {
		in := patternTar(t, tc.name, tc.compression, map[string]string{"vegeta_erigon_eth_call.txt": vegetaFile(call1)})
		got, err := convertToString(t, in, "")
		if err != nil {
			t.Errorf("%s compressed with %s: %v", tc.name, tc.compression, err)
			continue
		}
		if got != call1+"\n" {
			t.Errorf("%s compressed with %s: got %s", tc.name, tc.compression, got)
		}
	}
}

func TestConvertVegetaTextFile(t *testing.T) {
	in := filepath.Join(t.TempDir(), "vegeta_erigon_eth_call.txt")
	if err := os.WriteFile(in, []byte(vegetaFile(call1, call2)), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := convertToString(t, in, "")
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if got != call1+"\n"+call2+"\n" {
		t.Errorf("got %s", got)
	}
}

func TestConvertTarWithSeveralPatternsNeedsName(t *testing.T) {
	in := patternTar(t, "p.tar", "", map[string]string{
		"vegeta_erigon_debug_storageRangeAt.txt": vegetaFile(call1),
		"vegeta_erigon_eth_getBlockByNumber.txt": vegetaFile(call2),
	})
	_, err := convertToString(t, in, "")
	if err == nil || !strings.Contains(err.Error(), "debug_storageRangeAt") || !strings.Contains(err.Error(), "eth_getBlockByNumber") {
		t.Fatalf("got error %v, want one listing both patterns", err)
	}
	got, err := convertToString(t, in, "debug_storageRangeAt")
	if err != nil {
		t.Fatalf("convert with name: %v", err)
	}
	if got != call1+"\n" {
		t.Errorf("got %s, want the debug_storageRangeAt requests", got)
	}
	if _, err := convertToString(t, in, "eth_call"); err == nil {
		t.Error("name not in the tar: want an error")
	}
}

func TestConvertRejectsABodyThatIsNotJSON(t *testing.T) {
	in := filepath.Join(t.TempDir(), "v.txt")
	bad := `{"method": "POST", "url": "u", "body": "` + base64.StdEncoding.EncodeToString([]byte("not json")) + `"}`
	if err := os.WriteFile(in, []byte(vegetaFile(call1)+bad+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := convertToString(t, in, ""); err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("got error %v, want one naming line 2", err)
	}
}

func TestCommandWritesTheOutputOnlyOnSuccess(t *testing.T) {
	in := patternTar(t, "p.tar", "", map[string]string{"vegeta_erigon_eth_call.txt": vegetaFile(call1, call2)})
	out := filepath.Join(t.TempDir(), "eth_call.jsonl")
	if err := newApp().Run([]string{"rpc_pattern_convert", "--in", in, "--out", out}); err != nil {
		t.Fatalf("command: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil || string(data) != call1+"\n"+call2+"\n" {
		t.Fatalf("output %q, err %v", data, err)
	}

	bad := patternTar(t, "bad.tar", "", map[string]string{"results_erigon_eth_call.csv": "1, eth_call, 100\n"})
	missing := filepath.Join(t.TempDir(), "none.jsonl")
	if err := newApp().Run([]string{"rpc_pattern_convert", "--in", bad, "--out", missing}); err == nil {
		t.Error("tar without a vegeta_erigon file: want an error")
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Errorf("output written on error, stat err %v", err)
	}
}
