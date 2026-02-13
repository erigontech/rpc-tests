package main

import (
	"archive/tar"
	"compress/bzip2"
	"compress/gzip"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	bzip2w "github.com/dsnet/compress/bzip2"
)

// Supported compression types
const (
	GzipCompression  = ".gz"
	Bzip2Compression = ".bz2"
	NoCompression    = ""
)

// --- Helper Functions ---

// getCompressionType determines the compression from the filename extension.
func getCompressionType(filename string) string {
	if strings.HasSuffix(filename, ".tar.gz") || strings.HasSuffix(filename, ".tgz") {
		return GzipCompression
	}
	if strings.HasSuffix(filename, ".tar.bz2") || strings.HasSuffix(filename, ".tbz") {
		return Bzip2Compression
	}
	return NoCompression
}

// --- Archiving Logic ---

// createArchive creates a compressed or uncompressed tar archive.
func createArchive(archivePath string, files []string) error {
	fmt.Printf("📦 Creating archive: %s\n", archivePath)

	// 1. Create the output file
	outFile, err := os.Create(archivePath)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer outFile.Close()

	// 2. Wrap the output file with the correct compression writer
	var writer io.WriteCloser = outFile
	compressionType := getCompressionType(archivePath)

	switch compressionType {
	case GzipCompression:
		writer = gzip.NewWriter(outFile)
	case Bzip2Compression:
		config := &bzip2w.WriterConfig{Level: bzip2w.BestCompression}
		writer, err = bzip2w.NewWriter(outFile, config)
		if err != nil {
			return fmt.Errorf("failed to create bzip2 writer: %w", err)
		}
	}
	// For robustness in a real-world scenario, you'd check and defer Close() on the compression writer.
	// For this demonstration, we'll focus on the tar writer cleanup.

	// 3. Create the Tar writer
	tarWriter := tar.NewWriter(writer)
	defer tarWriter.Close()

	// 4. Add files to the archive
	for _, file := range files {
		err := addFileToTar(tarWriter, file, "")
		if err != nil {
			return fmt.Errorf("failed to add file %s: %w", file, err)
		}
	}

	// 5. Explicitly close the compression writer if it was used (before closing the tar writer)
	if compressionType != NoCompression {
		if err := writer.Close(); err != nil {
			return fmt.Errorf("failed to close compression writer: %w", err)
		}
	}

	return nil
}

// addFileToTar recursively adds a file or directory to the tar archive.
func addFileToTar(tarWriter *tar.Writer, filePath, baseDir string) error {
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		return err
	}

	// Determine the name inside the archive (relative path)
	var link string
	if fileInfo.Mode()&os.ModeSymlink != 0 {
		link, err = os.Readlink(filePath)
		if err != nil {
			return err
		}
	}

	// If baseDir is not empty, use the relative path, otherwise use the basename
	var nameInArchive string
	if baseDir != "" && strings.HasPrefix(filePath, baseDir) {
		nameInArchive = filePath[len(baseDir)+1:]
	} else {
		nameInArchive = filepath.Base(filePath)
	}

	// Create the Tar Header
	header, err := tar.FileInfoHeader(fileInfo, link)
	if err != nil {
		return err
	}
	header.Name = nameInArchive

	if err := tarWriter.WriteHeader(header); err != nil {
		return err
	}

	// Write file contents if it's a regular file
	if fileInfo.Mode().IsRegular() {
		file, err := os.Open(filePath)
		if err != nil {
			return err
		}
		defer file.Close()

		if _, err := io.Copy(tarWriter, file); err != nil {
			return err
		}
		fmt.Printf("   -> Added: %s\n", filePath)
	}

	// Recurse into directories
	if fileInfo.IsDir() {
		dirEntries, err := os.ReadDir(filePath)
		if err != nil {
			return err
		}
		for _, entry := range dirEntries {
			fullPath := filepath.Join(filePath, entry.Name())
			// Keep the original baseDir if it was set, otherwise set it to the current path's parent
			newBaseDir := baseDir
			if baseDir == "" {
				// Special handling for the root call: use the current path as the new base.
				// This ensures nested files have relative paths within the archive.
				newBaseDir = filePath
			}
			if err := addFileToTar(tarWriter, fullPath, newBaseDir); err != nil {
				return err
			}
		}
	}

	return nil
}

// --- Unarchiving Logic ---

func autodetectCompression(archivePath string, inFile *os.File) (string, error) {
	compressionType := NoCompression
	tarReader := tar.NewReader(inFile)
	_, err := tarReader.Next()
	if err != nil {
		inFile.Close()
		inFile, err = os.Open(archivePath)
		if err != nil {
			return compressionType, err
		}
		_, err = gzip.NewReader(inFile)
		if err == nil { // gzip is OK, rename
			compressionType = GzipCompression
			if err := inFile.Close(); err != nil {
				return compressionType, err
			}
		} else {
			inFile.Close()
			inFile, err = os.Open(archivePath)
			if err != nil {
				return compressionType, err
			}
			_, err = tar.NewReader(bzip2.NewReader(inFile)).Next()
			inFile.Close()
			if err == nil { // bzip2 is OK, rename
				compressionType = Bzip2Compression
			}
		}
	}
	return compressionType, nil
}

// extractArchive extracts a compressed or uncompressed tar archive.
func extractArchive(archivePath string, renameIfCompressed bool) error {
	fmt.Printf("📂 Extracting archive: %s\n", archivePath)

	// 1. Open the archive file
	inFile, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("failed to open archive: %w", err)
	}
	defer inFile.Close()

	// 2. Wrap the input file with the correct compression reader
	compressionType := getCompressionType(archivePath)
	if compressionType == NoCompression {
		// Handle the corner case where the file is compressed but has tar extension
		compressionType, err = autodetectCompression(archivePath, inFile)
		if err != nil {
			return fmt.Errorf("failed to autodetect compression for archive: %w", err)
		}
		if compressionType != NoCompression && renameIfCompressed {
			err = os.Rename(archivePath, archivePath+compressionType)
			if err != nil {
				return err
			}
			archivePath = archivePath + compressionType
		}
		inFile, err = os.Open(archivePath)
		if err != nil {
			return err
		}
	}

	var reader io.Reader
	switch compressionType {
	case GzipCompression:
		if reader, err = gzip.NewReader(inFile); err != nil {
			return fmt.Errorf("failed to create gzip reader: %w", err)
		}
		// gzip.NewReader has an implicit Close() that cleans up the internal state,
		// but since we wrap it in a tar reader, we rely on the tar reader for overall flow.
		// In a production scenario, you would defer the close of the gzip reader.
	case Bzip2Compression:
		reader = bzip2.NewReader(inFile)
	case NoCompression:
		reader = inFile
	}

	// 3. Create the Tar reader
	tarReader := tar.NewReader(reader)

	// 4. Iterate over files in the archive and extract them
	for {
		header, err := tarReader.Next()

		if err == io.EOF {
			break // End of archive
		}
		if err != nil {
			return fmt.Errorf("failed to read tar header: %w", err)
		}

		targetPath := filepath.Dir(archivePath) + "/" + header.Name

		switch header.Typeflag {
		case tar.TypeDir:
			// Create directory
			if err := os.MkdirAll(targetPath, os.FileMode(header.Mode)); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", targetPath, err)
			}
			fmt.Printf("   -> Created directory: %s\n", targetPath)

		case tar.TypeReg:
			// Ensure the parent directory exists before creating the file
			if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
				return fmt.Errorf("failed to create parent directory for %s: %w", targetPath, err)
			}

			// Create the file
			outFile, err := os.OpenFile(targetPath, os.O_CREATE|os.O_RDWR, os.FileMode(header.Mode))
			if err != nil {
				return fmt.Errorf("failed to create file %s: %w", targetPath, err)
			}

			// Write content
			if _, err := io.Copy(outFile, tarReader); err != nil {
				outFile.Close()
				return fmt.Errorf("failed to write file content for %s: %w", targetPath, err)
			}
			outFile.Close()
			fmt.Printf("   -> Extracted file: %s\n", targetPath)

		default:
			fmt.Printf("   -> Skipping unsupported file type %c: %s\n", header.Typeflag, targetPath)
		}
	}

	return nil
}

// --- Main Function and CLI ---

func main() {
	// Define command-line flags
	extractFlag := flag.Bool("x", false, "Extract (unarchive) files from the archive.")
	renameFlag := flag.Bool("r", false, "Rename the archive when extracting if it's compressed.")

	// The archive name is always the first non-flag argument
	flag.Usage = func() {
		_, _ = fmt.Fprintf(os.Stderr, "Usage:\n")
		_, _ = fmt.Fprintf(os.Stderr, "  Archive: %s <archive_name> <file_or_dir_1> [file_or_dir_2]...\n", os.Args[0])
		_, _ = fmt.Fprintf(os.Stderr, "  Unarchive: %s -x <archive_name>\n\n", os.Args[0])
		_, _ = fmt.Fprintf(os.Stderr, "Supported extensions: .tar, .tar.gz/.tgz, .tar.bz2/.tbz\n\n")
		_, _ = fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
	}

	flag.Parse()
	args := flag.Args()
	if len(args) < 1 {
		flag.Usage()
		os.Exit(1)
	}

	archivePath := args[0]

	if *extractFlag {
		// UNARCHIVE MODE (-x)
		if err := extractArchive(archivePath, *renameFlag); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "❌ Error during extraction: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("✅ Extraction complete.")
	} else {
		// ARCHIVE MODE (default)
		if len(args) < 2 {
			_, _ = fmt.Fprintf(os.Stderr, "Error: Must specify files/directories to archive.\n\n")
			flag.Usage()
			os.Exit(1)
		}
		filesToArchive := args[1:]
		if err := createArchive(archivePath, filesToArchive); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "❌ Error during archiving: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("✅ Archiving complete.")
	}
}
