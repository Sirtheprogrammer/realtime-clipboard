package api

import (
	"bytes"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"rsc.io/qr"
)

// quietZone is the four-module white margin the QR spec requires. Scanners rely
// on it to find the symbol, so it is not decoration.
const quietZone = 4

func (s *Server) handleRoomQR(w http.ResponseWriter, r *http.Request) {
	code, ok := NormalizeRoomCode(r.PathValue("code"))
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid room code")
		return
	}

	target := externalBaseURL(r) + "/r/" + code
	svg, err := qrSVG(target)
	if err != nil {
		s.log.Error("encode qr", "room", code, "err", err)
		writeError(w, http.StatusInternalServerError, "could not build a QR code")
		return
	}

	w.Header().Set("Content-Type", "image/svg+xml; charset=utf-8")
	// The room's URL does not change, but it is derived from the request host,
	// so keep this out of shared caches.
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Clipboard-QR-Target", target)
	_, _ = w.Write(svg)
}

// qrSVG renders text as an SVG QR code sized in module units, so the browser can
// scale it to any size and keep hard edges.
func qrSVG(text string) ([]byte, error) {
	code, err := qr.Encode(text, qr.M)
	if err != nil {
		return nil, fmt.Errorf("encode qr: %w", err)
	}

	dim := code.Size + quietZone*2

	var path bytes.Buffer
	for y := 0; y < code.Size; y++ {
		// Merge each row's dark modules into runs: one path segment per run
		// instead of per module keeps the document small.
		for x := 0; x < code.Size; {
			if !code.Black(x, y) {
				x++
				continue
			}
			run := 1
			for x+run < code.Size && code.Black(x+run, y) {
				run++
			}
			fmt.Fprintf(&path, "M%d %dh%dv1h-%dz",
				x+quietZone, y+quietZone, run, run)
			x += run
		}
	}

	var out bytes.Buffer
	fmt.Fprintf(&out, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" `+
		`width="%d" height="%d" shape-rendering="crispEdges" role="img" `+
		`aria-label="QR code for this clipboard room">`, dim, dim, dim*8, dim*8)
	// QR readers expect dark on light, so these colours are fixed rather than
	// following the page theme.
	fmt.Fprintf(&out, `<rect width="%d" height="%d" fill="#ffffff"/>`, dim, dim)
	fmt.Fprintf(&out, `<path d="%s" fill="#000000"/>`, path.String())
	out.WriteString(`</svg>`)

	return out.Bytes(), nil
}

// hostPattern accepts a hostname, an IPv4 address or a bracketed IPv6 address,
// each with an optional port.
var hostPattern = regexp.MustCompile(`^(?:\[[0-9a-fA-F:]+\]|[a-zA-Z0-9.-]+)(?::\d{1,5})?$`)

// externalBaseURL reconstructs the URL a phone would have to open.
//
// The dyno behind Heroku's router — like any app behind a TLS terminator — sees
// a plain HTTP request, so the scheme has to come from X-Forwarded-Proto or the
// QR would send phones to an http:// URL that redirects at best.
func externalBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	switch firstForwarded(r.Header.Get("X-Forwarded-Proto")) {
	case "https":
		scheme = "https"
	case "http":
		scheme = "http"
	}

	host := r.Host
	if forwarded := firstForwarded(r.Header.Get("X-Forwarded-Host")); forwarded != "" {
		host = forwarded
	}
	if !hostPattern.MatchString(host) {
		if hostPattern.MatchString(r.Host) {
			host = r.Host
		} else {
			host = "localhost"
		}
	}
	return scheme + "://" + host
}

// firstForwarded takes the leftmost value of a comma-separated proxy header,
// which is the one the client actually spoke to.
func firstForwarded(value string) string {
	if value == "" {
		return ""
	}
	first, _, _ := strings.Cut(value, ",")
	return strings.TrimSpace(first)
}
