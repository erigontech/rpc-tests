package archive

import (
	"archive/tar"
	"compress/bzip2"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// Compression defines the supported compression types
type Compression int

const (
	GzipCompression Compression = iota
	Bzip2Compression
	NoCompression
)

func (c Compression) String() string {
	return [...]string{"gzip", "bzip2", "none"}[c]
}

func (c Compression) Extension() string {
	return [...]string{".gz", ".bz2", ""}[c]
}

// getCompressionKind determines the compression from the filename extension.
func getCompressionKind(filename string) Compression {
	if strings.HasSuffix(filename, ".tar.gz") || strings.HasSuffix(filename, ".tgz") {
		return GzipCompression
	}
	if strings.HasSuffix(filename, ".tar.bz2") || strings.HasSuffix(filename, ".tbz") {
		return Bzip2Compression
	}
	return NoCompression
}

// autodetectCompression attempts to detect the compression type of the input file
func autodetectCompression(inFile *os.File) (Compression, error) {
	compressionType := NoCompression
	tarReader := tar.NewReader(inFile)
	_, err := tarReader.Next()
	if err != nil && !errors.Is(err, io.EOF) {
		// Reset the file position and check if it's gzip encoded
		_, err = inFile.Seek(0, io.SeekStart)
		if err != nil {
			return compressionType, err
		}
		_, err = gzip.NewReader(inFile)
		if err == nil {
			compressionType = GzipCompression
		} else {
			// Reset the file position and check if it's bzip2 encoded
			_, err = inFile.Seek(0, io.SeekStart)
			if err != nil {
				return compressionType, err
			}
			_, err = tar.NewReader(bzip2.NewReader(inFile)).Next()
			if err == nil {
				compressionType = Bzip2Compression
			}
		}
	}
	return compressionType, nil
}

// ExtractAndApply extracts a compressed or uncompressed tar archive and applies the given function to it.
func ExtractAndApply(archivePath string, sanitizeExtension bool, f func(*tar.Reader) error) error {
	inputFile, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("failed to open archive: %w", err)
	}
	defer func(inputFile *os.File) {
		err = inputFile.Close()
		if err != nil {
			fmt.Printf("Warning: failed to close input file: %v", err)
		}
	}(inputFile)

	// If the archive appears to be uncompressed, try to autodetect any compression type
	compressionKind := getCompressionKind(archivePath)
	if compressionKind == NoCompression {
		compressionKind, err = autodetectCompression(inputFile)
		if err != nil {
			return fmt.Errorf("failed to autodetect compression for archive: %w", err)
		}
		// Check if we are required to sanitise the extension for compressed archives
		if compressionKind != NoCompression && sanitizeExtension {
			err = os.Rename(archivePath, archivePath+compressionKind.Extension())
			if err != nil {
				return err
			}
			archivePath = archivePath + compressionKind.Extension()
		}
		// Reopening the file is necessary to reset the position and also because of potential renaming
		inputFile, err = os.Open(archivePath)
		if err != nil {
			return err
		}
	}

	var reader io.Reader
	switch compressionKind {
	case GzipCompression:
		gzReader, err := gzip.NewReader(inputFile)
		if err != nil {
			return fmt.Errorf("failed to create gzip reader: %w", err)
		}
		defer func(gzReader *gzip.Reader) {
			err = gzReader.Close()
			if err != nil {
				fmt.Printf("Warning: failed to close gzip reader: %v", err)
			}
		}(gzReader)
		reader = gzReader
	case Bzip2Compression:
		reader = bzip2.NewReader(inputFile)
	case NoCompression:
		reader = inputFile
	}

	tarReader := tar.NewReader(reader)
	header, err := tarReader.Next()
	if err == io.EOF {
		return fmt.Errorf("archive is empty")
	}
	if err != nil {
		return fmt.Errorf("failed to read tar header: %w", err)
	}
	if header.Typeflag != tar.TypeReg {
		return fmt.Errorf("expected regular file in archive, got type %v", header.Typeflag)
	}

	if err = f(tarReader); err != nil {
		return err
	}

	return nil
}
