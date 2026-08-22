package static

import (
	"archive/zip"
	"embed"
	"io"
	"net/http"
	"strings"
)

//go:embed all:extensions
var extensionFS embed.FS

// extensionFiles lists the files packaged into the verification-bridge zip.
// Test files are excluded.
var extensionFiles = []string{
	"manifest.json",
	"background.js",
	"bridge-common.js",
	"popup.html",
	"popup.css",
	"popup.js",
}

// WriteVerificationBridgeZip writes a zip archive of the verification-bridge
// extension to w, excluding test files.
func WriteVerificationBridgeZip(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="octopus-verification-bridge.zip"`)
	w.Header().Set("Cache-Control", "no-cache")

	zipWriter := zip.NewWriter(w)
	for _, name := range extensionFiles {
		data, err := extensionFS.ReadFile("extensions/verification-bridge/" + name)
		if err != nil {
			return err
		}
		entry, err := zipWriter.Create(name)
		if err != nil {
			return err
		}
		if _, err := io.Copy(entry, strings.NewReader(string(data))); err != nil {
			return err
		}
	}
	return zipWriter.Close()
}
