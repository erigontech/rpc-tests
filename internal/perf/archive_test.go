package perf

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	bzip2w "github.com/dsnet/compress/bzip2"
)

// writeTar builds a tar archive of name -> content at path, optionally
// compressed according to the compression constant.
func writeTar(t *testing.T, path, compression string, files map[string]string) {
	t.Helper()
	out, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	defer out.Close()

	var writer io.WriteCloser = nopWriteCloser{out}
	switch compression {
	case GzipCompression:
		writer = gzip.NewWriter(out)
	case Bzip2Compression:
		writer, err = bzip2w.NewWriter(out, &bzip2w.WriterConfig{Level: bzip2w.BestSpeed})
		if err != nil {
			t.Fatalf("bzip2 writer: %v", err)
		}
	}

	tw := tar.NewWriter(writer)
	// A directory entry exercises the tar.TypeDir branch of extractTarGz.
	if err := tw.WriteHeader(&tar.Header{
		Name: "erigon_stress_test", Typeflag: tar.TypeDir, Mode: 0755,
	}); err != nil {
		t.Fatalf("write dir header: %v", err)
	}
	for name, content := range files {
		if err := tw.WriteHeader(&tar.Header{
			Name: name, Mode: 0644, Size: int64(len(content)), Typeflag: tar.TypeReg,
		}); err != nil {
			t.Fatalf("write header %s: %v", name, err)
		}
		if _, err := io.WriteString(tw, content); err != nil {
			t.Fatalf("write body %s: %v", name, err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close compressor: %v", err)
	}
}

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

func TestExtractTarGz(t *testing.T) {
	tests := []struct {
		name        string
		filename    string
		compression string
	}{
		{"uncompressed tar", "pattern.tar", NoCompression},
		{"gzip by extension", "pattern.tar.gz", GzipCompression},
		{"tgz by extension", "pattern.tgz", GzipCompression},
		{"bzip2 by extension", "pattern.tar.bz2", Bzip2Compression},
		{"tbz by extension", "pattern.tbz", Bzip2Compression},
		// Compressed content behind a plain .tar name forces autodetection.
		{"gzip needing autodetection", "mislabelled.tar", GzipCompression},
		{"bzip2 needing autodetection", "mislabelled.tar", Bzip2Compression},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			archive := filepath.Join(dir, tt.filename)
			writeTar(t, archive, tt.compression, map[string]string{
				"erigon_stress_test/vegeta_erigon_eth_getLogs.txt": "target-line\n",
			})

			dest := filepath.Join(dir, "out")
			if err := os.MkdirAll(dest, 0755); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
			if err := extractTarGz(archive, dest); err != nil {
				t.Fatalf("extractTarGz: %v", err)
			}

			extracted := filepath.Join(dest, "erigon_stress_test", "vegeta_erigon_eth_getLogs.txt")
			data, err := os.ReadFile(extracted)
			if err != nil {
				t.Fatalf("extracted file missing: %v", err)
			}
			if string(data) != "target-line\n" {
				t.Errorf("content: got %q", data)
			}
		})
	}
}

func TestExtractTarGzMissingArchive(t *testing.T) {
	err := extractTarGz(filepath.Join(t.TempDir(), "absent.tar"), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "failed to open archive") {
		t.Errorf("got %v, want an open failure", err)
	}
}

func TestExtractTarGzCorruptGzip(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "broken.tar.gz")
	if err := os.WriteFile(archive, []byte("definitely not gzip"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := extractTarGz(archive, dir); err == nil {
		t.Error("expected an error for a corrupt gzip archive")
	}
}

func TestAutodetectCompression(t *testing.T) {
	tests := []struct {
		name        string
		compression string
		want        string
	}{
		{"plain tar", NoCompression, NoCompression},
		{"gzip", GzipCompression, GzipCompression},
		{"bzip2", Bzip2Compression, Bzip2Compression},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "archive.tar")
			writeTar(t, path, tt.compression, map[string]string{"a.txt": "x"})

			f, err := os.Open(path)
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			defer f.Close()

			got, err := autodetectCompression(f)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAutodetectCompressionUnrecognisedContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "garbage.tar")
	if err := os.WriteFile(path, []byte("neither tar nor gzip nor bzip2"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()

	got, err := autodetectCompression(f)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != NoCompression {
		t.Errorf("got %q, want no compression", got)
	}
}

func TestCopyFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "dst.txt")
	if err := os.WriteFile(src, []byte("payload"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := copyFile(src, dst); err != nil {
		t.Fatalf("copyFile: %v", err)
	}
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read copy: %v", err)
	}
	if string(data) != "payload" {
		t.Errorf("content: got %q, want %q", data, "payload")
	}
}

func TestCopyFileErrors(t *testing.T) {
	dir := t.TempDir()

	t.Run("missing source", func(t *testing.T) {
		if err := copyFile(filepath.Join(dir, "absent"), filepath.Join(dir, "out")); err == nil {
			t.Error("expected an error for a missing source")
		}
	})

	t.Run("unwritable destination", func(t *testing.T) {
		src := filepath.Join(dir, "src.txt")
		if err := os.WriteFile(src, []byte("x"), 0644); err != nil {
			t.Fatalf("write: %v", err)
		}
		if err := copyFile(src, filepath.Join(dir, "missing-dir", "out")); err == nil {
			t.Error("expected an error for an uncreatable destination")
		}
	})
}

func TestReplaceInFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pattern.txt")
	original := `{"url":"http://localhost:8545"}` + "\n" + `{"url":"http://localhost:8545/x"}`
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := replaceInFile(path, "localhost", "10.0.0.5"); err != nil {
		t.Fatalf("replaceInFile: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.Contains(string(data), "localhost") {
		t.Errorf("every occurrence should be replaced, got %q", data)
	}
	if strings.Count(string(data), "10.0.0.5") != 2 {
		t.Errorf("expected 2 replacements, got %q", data)
	}
}

func TestReplaceInFileMissingFile(t *testing.T) {
	if err := replaceInFile(filepath.Join(t.TempDir(), "absent"), "a", "b"); err == nil {
		t.Error("expected an error for a missing file")
	}
}
