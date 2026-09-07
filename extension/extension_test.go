package extension

import (
	"archive/zip"
	"bytes"
	"testing"
)

func TestZipArchive(t *testing.T) {
	data, err := ZipArchive()
	if err != nil {
		t.Fatalf("ZipArchive failed: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("ZipArchive returned empty slice")
	}

	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("invalid zip archive: %v", err)
	}

	expectedFiles := map[string]bool{
		"manifest.json": false,
		"background.js": false,
		"content.js":    false,
		"popup.html":    false,
		"popup.css":     false,
		"popup.js":      false,
		"README.md":     false,
	}

	for _, f := range reader.File {
		if _, ok := expectedFiles[f.Name]; ok {
			expectedFiles[f.Name] = true
		}
	}

	for name, found := range expectedFiles {
		if !found {
			t.Errorf("expected file %s in zip archive", name)
		}
	}
}
