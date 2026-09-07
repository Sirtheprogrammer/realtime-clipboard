package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"clipboard/extension"
)

const ExtensionCurrentVersion = "1.2.0"

func (s *Server) handleExtensionDownload(w http.ResponseWriter, r *http.Request) {
	data, err := extension.ZipArchive()
	if err != nil {
		s.log.Error("generate extension zip failed", "err", err)
		writeError(w, http.StatusInternalServerError, "failed to package extension")
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="clipboard-vault-extension.zip"`)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

func (s *Server) handleExtensionVersion(w http.ResponseWriter, r *http.Request) {
	base := externalBaseURL(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"version":      ExtensionCurrentVersion,
		"download_url": base + "/api/extension/download",
		"release_url":  "https://github.com/Sirtheprogrammer/realtime-clipboard/releases/tag/v" + ExtensionCurrentVersion,
		"update_url":   base + "/api/extension/updates.xml",
	})
}

// handleExtensionUpdatesXML serves Chrome / Edge / Chromium Omaha update manifest
func (s *Server) handleExtensionUpdatesXML(w http.ResponseWriter, r *http.Request) {
	base := externalBaseURL(r)
	w.Header().Set("Content-Type", "text/xml; charset=UTF-8")
	w.Header().Set("Cache-Control", "public, max-age=300")

	// Chromium Omaha update XML format
	fmt.Fprintf(w, `<?xml version='1.0' encoding='UTF-8'?>
<gupdate xmlns='http://www.google.com/update2/response' protocol='2.0'>
  <app appid='clipboard-vault-extension'>
    <updatecheck codebase='%s/api/extension/download' version='%s' />
  </app>
</gupdate>`, base, ExtensionCurrentVersion)
}

// handleExtensionUpdatesJSON serves Firefox Gecko Add-on update manifest
func (s *Server) handleExtensionUpdatesJSON(w http.ResponseWriter, r *http.Request) {
	base := externalBaseURL(r)
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	w.Header().Set("Cache-Control", "public, max-age=300")

	// Firefox Gecko Add-on update JSON manifest format
	manifest := map[string]any{
		"addons": map[string]any{
			"vault@clip.codesky.tech": map[string]any{
				"updates": []map[string]any{
					{
						"version":     ExtensionCurrentVersion,
						"update_link": base + "/api/extension/download",
					},
				},
			},
		},
	}
	_ = json.NewEncoder(w).Encode(manifest)
}
