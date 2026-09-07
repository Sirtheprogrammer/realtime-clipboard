package extension

import (
	"archive/zip"
	"bytes"
	"embed"
	"io"
	"io/fs"
	"sync"
)

//go:embed manifest.json background.js content.js popup.html popup.css popup.js README.md icons/*
var ExtensionFS embed.FS

var (
	zipOnce  sync.Once
	zipBytes []byte
	zipErr   error
)

// ZipArchive builds and caches an in-memory zip bundle of the extension files.
func ZipArchive() ([]byte, error) {
	zipOnce.Do(func() {
		var buf bytes.Buffer
		w := zip.NewWriter(&buf)

		err := fs.WalkDir(ExtensionFS, ".", func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if path == "." || path == "extension.go" {
				return nil
			}
			if d.IsDir() {
				return nil
			}
			file, err := ExtensionFS.Open(path)
			if err != nil {
				return err
			}
			defer file.Close()

			fWriter, err := w.Create(path)
			if err != nil {
				return err
			}
			_, err = io.Copy(fWriter, file)
			return err
		})

		if err != nil {
			zipErr = err
			return
		}

		if err := w.Close(); err != nil {
			zipErr = err
			return
		}

		zipBytes = buf.Bytes()
	})

	return zipBytes, zipErr
}
