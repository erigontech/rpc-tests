package archive

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/dsnet/compress/bzip2"
)

// Helper functions

func closeFile(t *testing.T, file *os.File) {
	t.Helper()

	err := file.Close()
	if err != nil {
		t.Fatalf("failed to close file %s: %v", file.Name(), err)
	}
}

func createTempTarFile(t *testing.T, content string, compression Compression) string {
	t.Helper()

	tmpFile, err := os.CreateTemp("", "test_*.tar")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	var writer io.Writer = tmpFile
	var gzWriter *gzip.Writer
	var bzWriter *bzip2.Writer

	if compression == GzipCompression {
		gzWriter = gzip.NewWriter(tmpFile)
		writer = gzWriter
	} else if compression == Bzip2Compression {
		bzWriter, err = bzip2.NewWriter(tmpFile, &bzip2.WriterConfig{Level: bzip2.BestCompression})
		if err != nil {
			t.Fatalf("failed to create bzip2 writer: %v", err)
		}
		writer = bzWriter
	}

	tarWriter := tar.NewWriter(writer)

	contentBytes := []byte(content)
	header := &tar.Header{
		Name: "test.json",
		Size: int64(len(contentBytes)),
		Mode: 0644,
	}

	if err := tarWriter.WriteHeader(header); err != nil {
		t.Fatalf("failed to write tar header: %v", err)
	}
	if _, err := tarWriter.Write(contentBytes); err != nil {
		t.Fatalf("failed to write tar content: %v", err)
	}

	err = tarWriter.Close()
	if err != nil {
		t.Fatalf("failed to close tar writer: %v", err)
	}
	if gzWriter != nil {
		err = gzWriter.Close()
		if err != nil {
			t.Fatalf("failed to close gzip writer: %v", err)
		}
	}
	if bzWriter != nil {
		err = bzWriter.Close()
		if err != nil {
			t.Fatalf("failed to close bzip2 writer: %v", err)
		}
	}
	err = tmpFile.Close()
	if err != nil {
		t.Fatalf("failed to close temp file: %v", err)
	}

	return tmpFile.Name()
}

func createTempTarWithJSON(t *testing.T, compression Compression) string {
	t.Helper()

	jsonContent := `[{"request":"dGVzdA==","response":{"result":"ok"},"result":"ok"}]`
	return createTempTarFile(t, jsonContent, compression)
}

func createEmptyTarFile(t *testing.T) string {
	t.Helper()

	tmpFile, err := os.CreateTemp("", "empty_*.tar")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	return tmpFile.Name()
}

func createTempTarWithDirectory(t *testing.T) string {
	t.Helper()

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "dir.tar")

	file, err := os.Create(tmpFile)
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	tarWriter := tar.NewWriter(file)

	header := &tar.Header{
		Name:     "testdir/",
		Typeflag: tar.TypeDir,
		Mode:     0755,
	}

	if err := tarWriter.WriteHeader(header); err != nil {
		t.Fatalf("failed to write tar header: %v", err)
	}

	err = tarWriter.Close()
	if err != nil {
		return ""
	}
	defer closeFile(t, file)

	return tmpFile
}

func removeTempFile(t *testing.T, path string) {
	err := os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("failed to remove temp file: %v", err)
	}
}

func TestGetCompressionType(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		expected Compression
	}{
		{"tar.gz extension", "file.tar.gz", GzipCompression},
		{"tgz extension", "file.tgz", GzipCompression},
		{"tar.bz2 extension", "file.tar.bz2", Bzip2Compression},
		{"tbz extension", "file.tbz", Bzip2Compression},
		{"tar extension", "file.tar", NoCompression},
		{"json extension", "file.json", NoCompression},
		{"no extension", "file", NoCompression},
		{"path with tar.gz", "/path/to/file.tar.gz", GzipCompression},
		{"path with tgz", "/path/to/file.tgz", GzipCompression},
		{"path with tar.bz2", "/path/to/file.tar.bz2", Bzip2Compression},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getCompressionKind(tt.filename)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestAutodetectCompression_UncompressedTar(t *testing.T) {
	tmpFilePath := createTempTarWithJSON(t, NoCompression)
	defer removeTempFile(t, tmpFilePath)

	file, err := os.Open(tmpFilePath)
	if err != nil {
		t.Fatalf("failed to open temp file: %v", err)
	}
	defer closeFile(t, file)

	compressionType, err := autodetectCompression(file)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if compressionType != NoCompression {
		t.Errorf("expected NoCompression, got %q", compressionType)
	}
}

func TestAutodetectCompression_GzipTar(t *testing.T) {
	tmpFile := createTempTarWithJSON(t, GzipCompression)
	defer removeTempFile(t, tmpFile)

	file, err := os.Open(tmpFile)
	if err != nil {
		t.Fatalf("failed to open temp file: %v", err)
	}
	defer closeFile(t, file)

	compressionKind, err := autodetectCompression(file)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if compressionKind != GzipCompression {
		t.Errorf("expected GzipCompression, got %q", compressionKind)
	}
}

func TestAutodetectCompression_InvalidFile(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "invalid_*.dat")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer removeTempFile(t, tmpFile.Name())

	_, err = tmpFile.Write([]byte("this is not a valid archive"))
	if err != nil {
		t.Fatalf("failed to write to temp file: %v", err)
	}
	defer closeFile(t, tmpFile)

	file, err := os.Open(tmpFile.Name())
	if err != nil {
		t.Fatalf("failed to open temp file: %v", err)
	}
	defer closeFile(t, file)

	compressionType, err := autodetectCompression(file)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Invalid data should return NoCompression
	if compressionType != NoCompression {
		t.Errorf("expected NoCompression for invalid file, got %q", compressionType)
	}
}

var nullTarFunc = func(*tar.Reader) error { return nil }

func TestExtract_NonExistentFile(t *testing.T) {
	err := Extract("/nonexistent/path/file.tar", false, nullTarFunc)
	if err == nil {
		t.Error("expected error for non-existent file")
	}
}

func TestExtract_UncompressedTar(t *testing.T) {
	tmpFilePath := createTempTarWithJSON(t, NoCompression)
	defer removeTempFile(t, tmpFilePath)

	err := Extract(tmpFilePath, false, nullTarFunc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExtract_GzipTar(t *testing.T) {
	tmpFile := createTempTarWithJSON(t, GzipCompression)
	defer removeTempFile(t, tmpFile)

	// Rename it to change its extension
	newPath := tmpFile + ".tar.gz"
	if err := os.Rename(tmpFile, newPath); err != nil {
		t.Fatalf("failed to rename file: %v", err)
	}
	defer removeTempFile(t, newPath)

	err := Extract(newPath, false, nullTarFunc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExtract_NilMetrics(t *testing.T) {
	tmpFile := createTempTarWithJSON(t, NoCompression)
	defer removeTempFile(t, tmpFile)

	err := Extract(tmpFile, false, nullTarFunc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExtract_EmptyArchive(t *testing.T) {
	tmpFile := createEmptyTarFile(t)
	defer removeTempFile(t, tmpFile)

	// Empty archive should return error since Next() is called internally
	err := Extract(tmpFile, false, nullTarFunc)
	if err == nil {
		t.Error("expected error for empty archive")
	}
}

func TestExtract_InvalidJSON(t *testing.T) {
	tmpFile := createTempTarFile(t, "invalid json content", NoCompression)
	defer removeTempFile(t, tmpFile)

	err := Extract(tmpFile, false, nullTarFunc)
	if err != nil {
		t.Fatalf("unexpected error from Extract: %v", err)
	}
}

func TestExtract_SanitizeExtension(t *testing.T) {
	tmpFile := createTempTarWithJSON(t, GzipCompression)
	defer removeTempFile(t, tmpFile)
	defer removeTempFile(t, tmpFile+".gz")

	err := Extract(tmpFile, true, nullTarFunc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check that the original file was renamed
	if _, err := os.Stat(tmpFile); !os.IsNotExist(err) {
		t.Error("expected file to not exist anymore after extraction")
	}
	if _, err := os.Stat(tmpFile + ".gz"); os.IsNotExist(err) {
		t.Error("expected file to be renamed with .gz extension")
	}
}

func TestExtract_DirectoryInArchive(t *testing.T) {
	tmpFile := createTempTarWithDirectory(t)
	defer removeTempFile(t, tmpFile)

	err := Extract(tmpFile, false, nullTarFunc)
	if err == nil {
		t.Error("expected error for directory in archive as unsupported")
	}
}

func TestExtract_TgzExtension(t *testing.T) {
	tmpFile := createTempTarWithJSON(t, GzipCompression)

	// Rename to .tgz
	tgzPath := tmpFile[:len(tmpFile)-4] + ".tgz"
	if err := os.Rename(tmpFile, tgzPath); err != nil {
		t.Fatalf("failed to rename: %v", err)
	}
	defer removeTempFile(t, tgzPath)

	err := Extract(tgzPath, false, nullTarFunc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExtract_AutodetectGzip(t *testing.T) {
	// Create gzip tar but with .tar extension (no compression hint)
	tmpFile := createTempTarWithJSON(t, GzipCompression)
	defer removeTempFile(t, tmpFile)
	defer removeTempFile(t, tmpFile+".gz")

	err := Extract(tmpFile, false, nullTarFunc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExtract_Bzip2Tar(t *testing.T) {
	tmpFile := createTempTarWithJSON(t, Bzip2Compression)
	defer removeTempFile(t, tmpFile)

	var callbackInvoked bool
	err := Extract(tmpFile, false, func(tr *tar.Reader) error {
		callbackInvoked = true
		// Verify we can read from the tar - Next() already called, second should be EOF
		_, err := tr.Next()
		if err != io.EOF {
			t.Errorf("expected io.EOF for second Next() call, got: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !callbackInvoked {
		t.Error("expected callback to be invoked")
	}
}

func TestGetCompressionType_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		expected string
	}{
		{"empty string", "", NoCompression.Extension()},
		{"just .gz", ".gz", NoCompression.Extension()},
		{"just .tgz", ".tgz", GzipCompression.Extension()},
		{"double extension tar.gz.gz", "file.tar.gz.gz", NoCompression.Extension()},
		{"case sensitive TAR.GZ", "file.TAR.GZ", NoCompression.Extension()},
		{"mixed case TaR.gZ", "file.TaR.gZ", NoCompression.Extension()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getCompressionKind(tt.filename)
			if result.Extension() != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestExtract_CorruptedGzip(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "corrupted.tar.gz")

	// Write corrupted gzip data
	file, err := os.Create(tmpFile)
	if err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	// Gzip magic number but corrupted content
	_, err = file.Write([]byte{0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00})
	if err != nil {
		t.Fatalf("failed to write to file %s: %v", tmpFile, err)
	}
	err = file.Close()
	if err != nil {
		t.Fatalf("failed to close file %s: %v", tmpFile, err)
	}

	err = Extract(tmpFile, false, nullTarFunc)
	if err == nil {
		t.Error("expected error for corrupted gzip")
	}
}

func BenchmarkGetCompressionType(b *testing.B) {
	filenames := []string{
		"file.tar.gz",
		"file.tgz",
		"file.tar.bz2",
		"file.tbz",
		"file.tar",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, f := range filenames {
			getCompressionKind(f)
		}
	}
}

func BenchmarkExtract(b *testing.B) {
	tmpDir := b.TempDir()
	tmpFile := filepath.Join(tmpDir, "bench.tar")

	jsonContent := `[{"request":"dGVzdA==","response":{"result":"ok"},"result":"ok"}]`

	file, _ := os.Create(tmpFile)
	tarWriter := tar.NewWriter(file)
	contentBytes := []byte(jsonContent)
	header := &tar.Header{
		Name: "test.json",
		Size: int64(len(contentBytes)),
		Mode: 0644,
	}
	err := tarWriter.WriteHeader(header)
	if err != nil {
		b.Fatalf("unexpected error writing header for %s: %v", tmpFile, err)
	}
	_, err = tarWriter.Write(contentBytes)
	if err != nil {
		b.Fatalf("unexpected error writing content for %s: %v", tmpFile, err)
	}
	err = tarWriter.Close()
	if err != nil {
		b.Fatalf("unexpected error closing tar writer for %s: %v", tmpFile, err)
	}
	err = file.Close()
	if err != nil {
		b.Fatalf("unexpected error closing file for %s: %v", tmpFile, err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := Extract(tmpFile, false, nullTarFunc)
		if err != nil {
			b.Fatalf("unexpected error: %v", err)
		}
	}
}

func TestExtract_LargeJSON(t *testing.T) {
	// Create a large JSON payload
	var buf bytes.Buffer
	buf.WriteString("[")
	for i := 0; i < 100_000; i++ {
		if i > 0 {
			buf.WriteString(",")
		}
		buf.WriteString(`{"request":"dGVzdA==","response":{"result":"ok"},"result":"ok"}`)
	}
	buf.WriteString("]")

	tmpFile := createTempTarFile(t, buf.String(), NoCompression)
	defer removeTempFile(t, tmpFile)

	err := Extract(tmpFile, false, nullTarFunc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExtract_CallbackError(t *testing.T) {
	tmpFile := createTempTarWithJSON(t, NoCompression)
	defer removeTempFile(t, tmpFile)

	expectedErr := io.ErrUnexpectedEOF
	err := Extract(tmpFile, false, func(tr *tar.Reader) error {
		return expectedErr
	})
	if !errors.Is(err, expectedErr) {
		t.Errorf("expected callback error to propagate, got: %v", err)
	}
}

func TestExtract_CallbackReadsContent(t *testing.T) {
	expectedContent := `{"test":"value"}`
	tmpFile := createTempTarFile(t, expectedContent, NoCompression)
	defer removeTempFile(t, tmpFile)

	var readContent []byte
	err := Extract(tmpFile, false, func(tr *tar.Reader) error {
		var err error
		readContent, err = io.ReadAll(tr)
		return err
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(readContent) != expectedContent {
		t.Errorf("expected content %q, got %q", expectedContent, string(readContent))
	}
}

func TestExtract_NonExistentFileCallbackNotCalled(t *testing.T) {
	callbackCalled := false
	err := Extract("/nonexistent/path/file.tar", false, func(tr *tar.Reader) error {
		callbackCalled = true
		return nil
	})
	if err == nil {
		t.Error("expected error for non-existent file")
	}
	if callbackCalled {
		t.Error("callback should not be called for non-existent file")
	}
}
