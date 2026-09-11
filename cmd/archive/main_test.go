package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGetCompressionType(t *testing.T) {
	tests := []struct {
		filename string
		want     string
	}{
		{"archive.tar.gz", GzipCompression},
		{"archive.tgz", GzipCompression},
		{"archive.tar.bz2", Bzip2Compression},
		{"archive.tbz", Bzip2Compression},
		{"archive.tar", NoCompression},
		{"archive", NoCompression},
		{"archive.gz.tar", NoCompression},
		{"", NoCompression},
	}
	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			if got := getCompressionType(tt.filename); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// sourceTree creates a directory holding two files and a nested subdirectory.
func sourceTree(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "payload")
	nested := filepath.Join(root, "nested")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	files := map[string]string{
		filepath.Join(root, "a.json"):      `{"a":1}`,
		filepath.Join(root, "b.json"):      `{"b":2}`,
		filepath.Join(nested, "deep.json"): `{"deep":true}`,
	}
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	return root
}

func TestArchiveRoundTrip(t *testing.T) {
	tests := []struct {
		name    string
		archive string
	}{
		{"uncompressed tar", "bundle.tar"},
		{"gzip", "bundle.tar.gz"},
		{"tgz", "bundle.tgz"},
		{"bzip2", "bundle.tar.bz2"},
		{"tbz", "bundle.tbz"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := sourceTree(t)
			workDir := t.TempDir()
			archivePath := filepath.Join(workDir, tt.archive)

			if err := createArchive(archivePath, []string{source}); err != nil {
				t.Fatalf("createArchive: %v", err)
			}
			info, err := os.Stat(archivePath)
			if err != nil {
				t.Fatalf("archive was not created: %v", err)
			}
			if info.Size() == 0 {
				t.Fatal("archive is empty")
			}

			// extractArchive writes next to the archive.
			if err := extractArchive(archivePath, false); err != nil {
				t.Fatalf("extractArchive: %v", err)
			}
			for name, want := range map[string]string{
				"a.json":           `{"a":1}`,
				"b.json":           `{"b":2}`,
				"nested/deep.json": `{"deep":true}`,
			} {
				data, err := os.ReadFile(filepath.Join(workDir, filepath.FromSlash(name)))
				if err != nil {
					t.Errorf("%s was not extracted: %v", name, err)
					continue
				}
				if string(data) != want {
					t.Errorf("%s: got %q, want %q", name, data, want)
				}
			}
		})
	}
}

func TestCreateArchiveSingleFile(t *testing.T) {
	workDir := t.TempDir()
	source := filepath.Join(workDir, "single.json")
	if err := os.WriteFile(source, []byte(`{"x":1}`), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	archivePath := filepath.Join(t.TempDir(), "one.tar.gz")
	if err := createArchive(archivePath, []string{source}); err != nil {
		t.Fatalf("createArchive: %v", err)
	}
	if err := extractArchive(archivePath, false); err != nil {
		t.Fatalf("extractArchive: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(archivePath), "single.json"))
	if err != nil {
		t.Fatalf("extracted file missing: %v", err)
	}
	if string(data) != `{"x":1}` {
		t.Errorf("content: got %q", data)
	}
}

func TestCreateArchiveMissingSource(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "bundle.tar")
	err := createArchive(archivePath, []string{filepath.Join(t.TempDir(), "absent")})
	if err == nil {
		t.Fatal("expected an error for a missing source")
	}
	if !strings.Contains(err.Error(), "failed to add file") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCreateArchiveUnwritableDestination(t *testing.T) {
	err := createArchive(filepath.Join(t.TempDir(), "missing-dir", "bundle.tar"), []string{sourceTree(t)})
	if err == nil {
		t.Error("expected an error when the archive cannot be created")
	}
}

func TestExtractArchiveMissingFile(t *testing.T) {
	err := extractArchive(filepath.Join(t.TempDir(), "absent.tar"), false)
	if err == nil {
		t.Fatal("expected an error for a missing archive")
	}
	if !strings.Contains(err.Error(), "failed to open archive") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestExtractArchiveAutodetectsCompression covers the corner case the tool was
// written for: compressed content behind a plain .tar name.
func TestExtractArchiveAutodetectsCompression(t *testing.T) {
	for _, ext := range []string{".tar.gz", ".tar.bz2"} {
		t.Run(ext, func(t *testing.T) {
			source := sourceTree(t)
			workDir := t.TempDir()

			// Build a compressed archive, then strip the telltale extension.
			compressed := filepath.Join(workDir, "bundle"+ext)
			if err := createArchive(compressed, []string{source}); err != nil {
				t.Fatalf("createArchive: %v", err)
			}
			mislabelled := filepath.Join(workDir, "bundle.tar")
			if err := os.Rename(compressed, mislabelled); err != nil {
				t.Fatalf("rename: %v", err)
			}

			if err := extractArchive(mislabelled, false); err != nil {
				t.Fatalf("extractArchive: %v", err)
			}
			if _, err := os.ReadFile(filepath.Join(workDir, "a.json")); err != nil {
				t.Errorf("archive was not extracted: %v", err)
			}
		})
	}
}

// TestExtractArchiveRenamesWhenCompressed covers the -r flag, which fixes up a
// mislabelled archive's extension in place.
func TestExtractArchiveRenamesWhenCompressed(t *testing.T) {
	source := sourceTree(t)
	workDir := t.TempDir()

	compressed := filepath.Join(workDir, "bundle.tar.gz")
	if err := createArchive(compressed, []string{source}); err != nil {
		t.Fatalf("createArchive: %v", err)
	}
	mislabelled := filepath.Join(workDir, "bundle.tar")
	if err := os.Rename(compressed, mislabelled); err != nil {
		t.Fatalf("rename: %v", err)
	}

	if err := extractArchive(mislabelled, true); err != nil {
		t.Fatalf("extractArchive: %v", err)
	}
	if _, err := os.Stat(mislabelled); !os.IsNotExist(err) {
		t.Errorf("the mislabelled archive should have been renamed, stat err = %v", err)
	}
	if _, err := os.Stat(mislabelled + GzipCompression); err != nil {
		t.Errorf("renamed archive is missing: %v", err)
	}
}

func TestAutodetectCompression(t *testing.T) {
	tests := []struct {
		name    string
		archive string
		want    string
	}{
		{"plain tar", "bundle.tar", NoCompression},
		{"gzip", "bundle.tar.gz", GzipCompression},
		{"bzip2", "bundle.tar.bz2", Bzip2Compression},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := sourceTree(t)
			path := filepath.Join(t.TempDir(), tt.archive)
			if err := createArchive(path, []string{source}); err != nil {
				t.Fatalf("createArchive: %v", err)
			}

			file, err := os.Open(path)
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			defer file.Close()

			got, err := autodetectCompression(path, file)
			if err != nil {
				t.Fatalf("autodetectCompression: %v", err)
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
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer file.Close()

	got, err := autodetectCompression(path, file)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != NoCompression {
		t.Errorf("got %q, want no compression", got)
	}
}

func TestExtractArchiveCorruptGzip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "broken.tar.gz")
	if err := os.WriteFile(path, []byte("definitely not gzip"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := extractArchive(path, false); err == nil {
		t.Error("expected an error for a corrupt gzip archive")
	}
}
