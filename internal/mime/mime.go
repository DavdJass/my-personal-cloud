package mime

import (
	"mime"
	"net/http"
	"strings"
)

// DetectReader reads the first 512 bytes (the "magic number" window) of src
// and returns the detected MIME type using net/http's built-in sniffing.
// The caller must use the returned *bytes.Reader as the replacement source
// if it needs to re-read the data.
func DetectReader(buf []byte) string {
	// net/http.DetectContentType uses the first 512 bytes.
	return http.DetectContentType(buf)
}

// DetectFromBytes wraps DetectReader for pre-buffered data.
func DetectFromBytes(b []byte) string {
	if len(b) > 512 {
		b = b[:512]
	}
	return DetectReader(b)
}

// IsImage checks whether the MIME type represents a raster image format.
func IsImage(m string) bool {
	return strings.HasPrefix(strings.ToLower(m), "image/")
}

// ExtensionForMIME returns a sane file extension for the MIME type.
func ExtensionForMIME(m string) string {
	exts, _ := mime.ExtensionsByType(m)
	if len(exts) > 0 {
		return exts[0]
	}
	return ""
}

// SniffSize is the number of bytes needed for content-type detection.
const SniffSize = 512
