package gate

import (
	"net/http"
	"path"
	"strings"
)

// Keep native previews limited to passive formats. Do not use the host's MIME
// registry: HTML, SVG and scripts must never execute under the session origin.
var nativePreviewTypes = map[string]string{
	".pdf": "application/pdf",
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg",
	".gif": "image/gif", ".webp": "image/webp", ".avif": "image/avif",
	".bmp": "image/bmp", ".ico": "image/x-icon",
	".mp3": "audio/mpeg", ".wav": "audio/wav", ".ogg": "audio/ogg",
	".oga": "audio/ogg", ".opus": "audio/ogg", ".flac": "audio/flac",
	".m4a": "audio/mp4", ".aac": "audio/aac",
	".mp4": "video/mp4", ".m4v": "video/mp4", ".webm": "video/webm",
	".ogv": "video/ogg",
}

func previewType(name string, sample []byte) (contentType string, inline bool) {
	ext := strings.ToLower(path.Ext(name))
	if contentType, ok := nativePreviewTypes[ext]; ok {
		return contentType, true
	}
	switch ext {
	case ".txt", ".log", ".md", ".markdown", ".csv", ".tsv", ".json", ".jsonc",
		".yaml", ".yml", ".toml", ".ini", ".conf", ".cfg", ".properties",
		".html", ".htm", ".svg", ".xml", ".css", ".js", ".mjs", ".ts", ".tsx", ".jsx",
		".go", ".c", ".h", ".cc", ".cpp", ".hpp", ".py", ".sh", ".bash",
		".sql", ".rs", ".java", ".bat", ".ps1", ".tex", ".rst", ".srt", ".vtt":
		return plainTextType(sample), true
	}
	// Sniff only the initial 512 bytes for extensionless or unfamiliar names.
	// Even detected HTML/XML must be presented as text, never executable content.
	detected := http.DetectContentType(sample)
	if strings.HasPrefix(detected, "text/") {
		return plainTextType(sample), true
	}
	for _, passiveType := range nativePreviewTypes {
		if detected == passiveType {
			return detected, true
		}
	}
	switch detected {
	case "audio/wave":
		return "audio/wav", true
	case "application/ogg":
		return detected, true
	}
	return "application/octet-stream", false
}

func plainTextType(sample []byte) string {
	// Let the browser decode UTF-16 text when a BOM identifies its byte order.
	if len(sample) >= 2 {
		if sample[0] == 0xff && sample[1] == 0xfe {
			return "text/plain; charset=utf-16le"
		}
		if sample[0] == 0xfe && sample[1] == 0xff {
			return "text/plain; charset=utf-16be"
		}
	}
	return "text/plain; charset=utf-8"
}
