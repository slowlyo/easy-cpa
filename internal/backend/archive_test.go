package backend

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

// TestExtractZipFileKeepsBinaryName 验证 zip 解压后的文件名定位。
func TestExtractZipFileKeepsBinaryName(t *testing.T) {
	root := t.TempDir()
	archivePath := filepath.Join(root, "core.zip")
	writer, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("create archive failed: %v", err)
	}

	zipWriter := zip.NewWriter(writer)
	entry, err := zipWriter.Create("nested/cli-proxy-api.exe")
	if err != nil {
		t.Fatalf("create zip entry failed: %v", err)
	}
	if _, err := entry.Write([]byte("binary")); err != nil {
		t.Fatalf("write zip entry failed: %v", err)
	}
	if err := zipWriter.Close(); err != nil {
		t.Fatalf("close zip failed: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close archive file failed: %v", err)
	}

	target := filepath.Join(root, "target")
	if err := extractCoreAsset(archivePath, target); err != nil {
		t.Fatalf("extract failed: %v", err)
	}
	if !FileExists(filepath.Join(target, "cli-proxy-api.exe")) {
		t.Fatalf("expected extracted binary at root target directory")
	}
}
