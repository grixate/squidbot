package management

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed ui/dist/*
var uiDist embed.FS

func assetExists(name string) bool {
	normalized := path.Clean(strings.TrimPrefix(name, "/"))
	_, err := fs.Stat(uiDist, normalized)
	return err == nil
}

func serveAsset(w http.ResponseWriter, name string) {
	normalized := path.Clean(strings.TrimPrefix(name, "/"))
	if strings.HasSuffix(normalized, ".css") {
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
	}
	if strings.HasSuffix(normalized, ".js") {
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	}
	if strings.HasSuffix(normalized, ".html") {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	}
	bytes, err := uiDist.ReadFile(normalized)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	_, _ = w.Write(bytes)
}
