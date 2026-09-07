package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"clipboard/extension"
)

const ExtensionCurrentVersion = "1.1.0"

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
	_, _ = w.Write(data)
}

func (s *Server) handleExtensionVersion(w http.ResponseWriter, r *http.Request) {
	baseURL := externalBaseURL(r)
	info := map[string]any{
		"version":            ExtensionCurrentVersion,
		"download_url":       baseURL + "/api/extension/download",
		"release_url":        "https://github.com/Sirtheprogrammer/realtime-clipboard/releases",
		"chrome_update_url":  baseURL + "/api/extension/updates.xml",
		"firefox_update_url": baseURL + "/api/extension/updates.json",
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	_ = json.NewEncoder(w).Encode(info)
}

func (s *Server) handleExtensionUpdatesXML(w http.ResponseWriter, r *http.Request) {
	baseURL := externalBaseURL(r)
	xmlResp := fmt.Sprintf(`<?xml version='1.0' encoding='UTF-8'?>
<gupdate xmlns='http://www.google.com/update2/response' protocol='2.0'>
  <app appid='clipboard-vault-extension'>
    <updatecheck codebase='%s/api/extension/download' version='%s' />
  </app>
</gupdate>`, baseURL, ExtensionCurrentVersion)

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(xmlResp))
}

func (s *Server) handleExtensionUpdatesJSON(w http.ResponseWriter, r *http.Request) {
	baseURL := externalBaseURL(r)
	jsonResp := map[string]any{
		"addons": map[string]any{
			"clipboard-vault@local": map[string]any{
				"updates": []map[string]any{
					{
						"version":     ExtensionCurrentVersion,
						"update_link": baseURL + "/api/extension/download",
					},
				},
			},
		},
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	_ = json.NewEncoder(w).Encode(jsonResp)
}
