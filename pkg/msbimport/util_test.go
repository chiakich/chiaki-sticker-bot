package msbimport

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func TestArchiveExtractZIPWithChineseFilename(t *testing.T) {
	dir := t.TempDir()
	archivePath := filepath.Join(dir, "source.zip")
	archive, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(archive)
	entry, err := writer.CreateHeader(&zip.FileHeader{
		Name:    "中文.png",
		NonUTF8: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte("png fixture")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}

	files := ArchiveExtract(archivePath)
	if len(files) != 1 {
		t.Fatalf("ArchiveExtract() returned %v, want one file", files)
	}
	if got, want := filepath.Base(files[0]), "中文.png"; got != want {
		t.Fatalf("extracted filename = %q, want %q", got, want)
	}
}
