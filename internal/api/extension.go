package api

import (
	"fmt"
	"net/http"

	"clipboard/extension"
)

func (s *Server) handleExtensionDownload(w http.ResponseWriter, r *http.Request) {
	data, err := extension.ZipArchive()
	if err != nil {
		s.log.Error("generate extension zip failed", "err", err)
		writeError(w, http.StatusInternalServerError, "failed to package extension")
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", "attachment; filename=\"clipboard-vault-extension.zip\"")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
