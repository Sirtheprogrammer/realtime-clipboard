package api

import (
	"crypto/tls"
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"rsc.io/qr"
)

// The encoder itself is rsc.io/qr's business. What these tests own is the
// rendering — that the SVG reproduces the encoder's matrix exactly — and the
// URL the QR points at, which is easy to get wrong behind a proxy.

var pathSegment = regexp.MustCompile(`M(\d+) (\d+)h(\d+)v1h-(\d+)z`)

// parseQRSVG reads a rendered SVG back into a module matrix, stripping the
// quiet zone, so it can be compared with what the encoder produced.
func parseQRSVG(t *testing.T, svg []byte, size int) [][]bool {
	t.Helper()

	matrix := make([][]bool, size)
	for i := range matrix {
		matrix[i] = make([]bool, size)
	}

	start := strings.Index(string(svg), `<path d="`)
	if start < 0 {
		t.Fatal("no path element in the rendered SVG")
	}
	rest := string(svg)[start+len(`<path d="`):]
	end := strings.Index(rest, `"`)
	if end < 0 {
		t.Fatal("unterminated path data")
	}

	for _, m := range pathSegment.FindAllStringSubmatch(rest[:end], -1) {
		x, _ := strconv.Atoi(m[1])
		y, _ := strconv.Atoi(m[2])
		run, _ := strconv.Atoi(m[3])
		if back, _ := strconv.Atoi(m[4]); back != run {
			t.Fatalf("path segment does not close: h%d then h-%d", run, back)
		}
		for i := 0; i < run; i++ {
			col, row := x-quietZone+i, y-quietZone
			if col < 0 || col >= size || row < 0 || row >= size {
				t.Fatalf("module (%d,%d) falls outside the symbol", col, row)
			}
			matrix[row][col] = true
		}
	}
	return matrix
}

func TestQRSVGReproducesTheEncodersMatrix(t *testing.T) {
	for _, text := range qrFixtures {
		t.Run(text, func(t *testing.T) {
			code, err := qr.Encode(text, qr.M)
			if err != nil {
				t.Fatalf("encode: %v", err)
			}

			svg, err := qrSVG(text)
			if err != nil {
				t.Fatalf("render: %v", err)
			}

			got := parseQRSVG(t, svg, code.Size)
			for y := 0; y < code.Size; y++ {
				for x := 0; x < code.Size; x++ {
					if got[y][x] != code.Black(x, y) {
						t.Fatalf("module (%d,%d): rendered %v, encoder says %v",
							x, y, got[y][x], code.Black(x, y))
					}
				}
			}
		})
	}
}

func TestQRSVGHasAQuietZone(t *testing.T) {
	text := qrFixtures[0]
	code, err := qr.Encode(text, qr.M)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	svg, err := qrSVG(text)
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	// Scanners use the four-module margin to find the symbol; without it many
	// readers simply never lock on.
	wantDim := code.Size + quietZone*2
	if want := fmt.Sprintf(`viewBox="0 0 %d %d"`, wantDim, wantDim); !strings.Contains(string(svg), want) {
		t.Errorf("expected %s in the SVG", want)
	}

	// Every drawn module must sit inside the margin.
	for _, m := range pathSegment.FindAllStringSubmatch(string(svg), -1) {
		x, _ := strconv.Atoi(m[1])
		y, _ := strconv.Atoi(m[2])
		run, _ := strconv.Atoi(m[3])
		if x < quietZone || y < quietZone || x+run > quietZone+code.Size {
			t.Fatalf("module run at (%d,%d) length %d intrudes on the quiet zone", x, y, run)
		}
	}
}

func TestQRSVGIsWellFormedXML(t *testing.T) {
	svg, err := qrSVG(qrFixtures[0])
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	decoder := xml.NewDecoder(strings.NewReader(string(svg)))
	for {
		_, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("malformed SVG: %v", err)
		}
	}
	// Dark on light, regardless of the page theme — readers depend on it.
	if !strings.Contains(string(svg), `fill="#ffffff"`) || !strings.Contains(string(svg), `fill="#000000"`) {
		t.Error("expected explicit black-on-white fills")
	}
}

func TestExternalBaseURL(t *testing.T) {
	tests := []struct {
		name    string
		host    string
		tls     bool
		headers map[string]string
		want    string
	}{
		{
			name: "plain http",
			host: "192.168.1.42:8080",
			want: "http://192.168.1.42:8080",
		},
		{
			name: "direct TLS",
			host: "clip.example.com",
			tls:  true,
			want: "https://clip.example.com",
		},
		{
			// The dyno sees plain HTTP; only the header knows it was HTTPS.
			name:    "behind a TLS terminator",
			host:    "your-clipboard.herokuapp.com",
			headers: map[string]string{"X-Forwarded-Proto": "https"},
			want:    "https://your-clipboard.herokuapp.com",
		},
		{
			name:    "proxy chain uses the leftmost value",
			host:    "internal:8080",
			headers: map[string]string{"X-Forwarded-Proto": "https, http"},
			want:    "https://internal:8080",
		},
		{
			name: "forwarded host wins",
			host: "internal:8080",
			headers: map[string]string{
				"X-Forwarded-Proto": "https",
				"X-Forwarded-Host":  "clip.example.com",
			},
			want: "https://clip.example.com",
		},
		{
			name:    "a junk forwarded host falls back to the real one",
			host:    "clip.example.com",
			headers: map[string]string{"X-Forwarded-Host": "evil.com/path?x=1"},
			want:    "http://clip.example.com",
		},
		{
			name: "ipv6 literal",
			host: "[fe80::1]:8080",
			want: "http://[fe80::1]:8080",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/api/rooms/abcd-efgh/qr.svg", nil)
			r.Host = tc.host
			for k, v := range tc.headers {
				r.Header.Set(k, v)
			}
			if tc.tls {
				r.TLS = &tlsState
			}
			if got := externalBaseURL(r); got != tc.want {
				t.Errorf("externalBaseURL() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestHandleRoomQR(t *testing.T) {
	s := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}

	t.Run("serves an SVG for the room URL", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/api/rooms/abcd-efgh/qr.svg", nil)
		r.Host = "clip.example.com"
		r.Header.Set("X-Forwarded-Proto", "https")
		r.SetPathValue("code", "abcd-efgh")

		w := httptest.NewRecorder()
		s.handleRoomQR(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d", w.Code)
		}
		if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "image/svg+xml") {
			t.Errorf("content type = %q", ct)
		}
		if target := w.Header().Get("X-Clipboard-QR-Target"); target != "https://clip.example.com/r/abcd-efgh" {
			t.Errorf("encoded target = %q", target)
		}
		if !strings.HasPrefix(w.Body.String(), "<svg") {
			t.Error("body is not an SVG")
		}
	})

	t.Run("rejects a bad room code", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/api/rooms/x/qr.svg", nil)
		r.SetPathValue("code", "../../etc/passwd")
		w := httptest.NewRecorder()
		s.handleRoomQR(w, r)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", w.Code)
		}
	})
}

// TestDumpQRSVGs writes the fixtures to disk so they can be checked with an
// outside decoder — the one thing a Go test cannot do for itself. It is inert
// unless QR_SVG_DUMP_DIR is set:
//
//	QR_SVG_DUMP_DIR=/tmp/qr go test ./internal/api/ -run TestDumpQRSVGs
//
// then decode the files with any scanner and confirm each one reads back as the
// URL in its name.
func TestDumpQRSVGs(t *testing.T) {
	dir := os.Getenv("QR_SVG_DUMP_DIR")
	if dir == "" {
		t.Skip("set QR_SVG_DUMP_DIR to write the fixtures out for external verification")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create dump dir: %v", err)
	}
	for i, text := range qrFixtures {
		svg, err := qrSVG(text)
		if err != nil {
			t.Fatalf("render %q: %v", text, err)
		}
		name := filepath.Join(dir, fmt.Sprintf("qr-%d.svg", i))
		if err := os.WriteFile(name, svg, 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		if err := os.WriteFile(name+".txt", []byte(text), 0o644); err != nil {
			t.Fatalf("write %s.txt: %v", name, err)
		}
	}
	t.Logf("wrote %d QR codes to %s", len(qrFixtures), dir)
}

// tlsState stands in for a direct HTTPS connection; only its presence matters.
var tlsState = tls.ConnectionState{}

var qrFixtures = []string{
	"https://clip.example.com/r/kfrb-mp3x",
	"http://192.168.1.42:8080/r/abcd-efgh",
	"https://your-clipboard.herokuapp.com/r/qwer-tyui",
	"http://localhost:8080/r/test-room",
}
