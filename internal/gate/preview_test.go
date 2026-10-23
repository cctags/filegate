package gate

import (
	"mime"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFilePreviewAndDownload(t *testing.T) {
	a := setup(t)
	cookie := access(t, a)
	cases := []struct{ name, data, contentType, disposition string }{
		{"日志.log", "中文日志\n", "text/plain; charset=utf-8", "inline"},
		{"readme.md", "# Heading\n", "text/plain; charset=utf-8", "inline"},
		{"data.json", `{"key":1}`, "text/plain; charset=utf-8", "inline"},
		{"config.jsonc", "// comment\n{}", "text/plain; charset=utf-8", "inline"},
		{"main.go", "package main\n", "text/plain; charset=utf-8", "inline"},
		{"page.HTML", `<script>fetch('/logout')</script>`, "text/plain; charset=utf-8", "inline"},
		{"image.svg", `<svg onload="alert(1)"></svg>`, "text/plain; charset=utf-8", "inline"},
		{"markup.xml", `<?xml version="1.0"?><a/>`, "text/plain; charset=utf-8", "inline"},
		{"README", "无扩展名文本\n", "text/plain; charset=utf-8", "inline"},
		{"page.unknown", "<!DOCTYPE html><script>alert(1)</script>", "text/plain; charset=utf-8", "inline"},
		{"报告.PDF", "%PDF-1.7\n", "application/pdf", "inline"},
		{"picture.png", "\x89PNG\r\n\x1a\n", "image/png", "inline"},
		{"movie.mp4", "video fixture", "video/mp4", "inline"},
		{"movie.webm", "video fixture", "video/webm", "inline"},
		{"music.mp3", "audio fixture", "audio/mpeg", "inline"},
		{"archive.zip", "PK\x03\x04\x00\x00", "application/octet-stream", "attachment"},
		{"document.docx", "PK\x03\x04\x00\x00", "application/octet-stream", "attachment"},
		{"unknown", "\x00\x01\x02", "application/octet-stream", "attachment"},
		{"empty", "", "text/plain; charset=utf-8", "inline"},
		{"utf16.txt", "\xff\xfe\x2d\x4e", "text/plain; charset=utf-16le", "inline"},
		{"utf16be", "\xfe\xff\x4e\x2d", "text/plain; charset=utf-16be", "inline"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(filepath.Join(a.cfg.Root, "ab", tc.name), []byte(tc.data), 0600); err != nil {
				t.Fatal(err)
			}
			target := (&url.URL{Path: "/file/" + tc.name}).String()
			for _, download := range []bool{false, true} {
				location := target
				if download {
					location += "?download=1"
				}
				w := request(a, http.MethodGet, location, "", cookie)
				if w.Code != 200 || w.Body.String() != tc.data {
					t.Fatalf("file content changed: %d %q", w.Code, w.Body.String())
				}
				if got := w.Header().Get("Content-Type"); got != tc.contentType {
					t.Errorf("Content-Type=%q, want %q", got, tc.contentType)
				}
				disposition, params, err := mime.ParseMediaType(w.Header().Get("Content-Disposition"))
				if err != nil {
					t.Fatal(err)
				}
				want := tc.disposition
				if download {
					want = "attachment"
				}
				if disposition != want || params["filename"] != tc.name {
					t.Errorf("unexpected disposition: %s %v", disposition, params)
				}
				if w.Header().Get("X-Content-Type-Options") != "nosniff" {
					t.Fatal("content sniffing protection missing")
				}
			}
		})
	}
	listing := request(a, "GET", "/browse/", "", cookie).Body.String()
	if !strings.Contains(listing, "?download=1") || !strings.Contains(listing, ">下载</a>") {
		t.Fatal("directory lacks explicit download links")
	}
	if w := request(a, "GET", "/file/hello.txt?download=1", "", nil); w.Code != 303 {
		t.Fatal("download bypassed authentication")
	}
}

func TestPreviewSniffingAndRange(t *testing.T) {
	a := setup(t)
	cookie := access(t, a)
	for name, data := range map[string]string{"noextpdf": "%PDF-1.7\n", "noextpng": "\x89PNG\r\n\x1a\n", "notes.log": "0123456789"} {
		if err := os.WriteFile(filepath.Join(a.cfg.Root, "ab", name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for name, want := range map[string]string{"noextpdf": "application/pdf", "noextpng": "image/png"} {
		w := request(a, "GET", "/file/"+name, "", cookie)
		if w.Header().Get("Content-Type") != want {
			t.Errorf("%s was not detected: %s", name, w.Header().Get("Content-Type"))
		}
	}
	for _, suffix := range []string{"", "?download=1"} {
		r := httptest.NewRequest("GET", "/file/notes.log"+suffix, nil)
		r.RemoteAddr = "192.0.2.1:1234"
		r.AddCookie(cookie)
		r.Header.Set("Range", "bytes=3-6")
		w := httptest.NewRecorder()
		a.Handler().ServeHTTP(w, r)
		if w.Code != 206 || w.Body.String() != "3456" {
			t.Fatalf("range broken: %d %q", w.Code, w.Body.String())
		}
		head := request(a, "HEAD", "/file/notes.log"+suffix, "", cookie)
		if head.Code != 200 || head.Body.Len() != 0 || head.Header().Get("Content-Length") != "10" {
			t.Fatal("HEAD broken")
		}
	}
}

func TestDownloadFilenameEscaping(t *testing.T) {
	a := setup(t)
	cookie := access(t, a)
	name := `文件 "<&>?#.txt`
	if err := os.WriteFile(filepath.Join(a.cfg.Root, "ab", name), []byte("safe text"), 0600); err != nil {
		t.Fatal(err)
	}
	fileURL := (&url.URL{Path: "/file/" + name, RawQuery: "download=1"}).String()
	w := request(a, "GET", fileURL, "", cookie)
	_, params, err := mime.ParseMediaType(w.Header().Get("Content-Disposition"))
	if err != nil || params["filename"] != name {
		t.Fatalf("filename lost: %v %v", params, err)
	}
	listing := request(a, "GET", "/browse/", "", cookie).Body.String()
	if strings.Contains(listing, `文件 "<&>`) {
		t.Fatal("filename was not HTML escaped")
	}
	if !strings.Contains(listing, "%3F%23.txt?download=1") {
		t.Fatal("download path query was not escaped")
	}
	// Both preview and explicit download must continue enforcing blacklists.
	if out := a.Admin(Request{Action: "block", Value: "192.0.2.1"}); out.Error != "" {
		t.Fatal(out.Error)
	}
	if request(a, "GET", fileURL, "", cookie).Code != 403 {
		t.Fatal("download bypassed blacklist")
	}
}
