package skills

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type ZipCache struct {
	root string
}

func NewZipCache(root string) *ZipCache {
	return &ZipCache{root: filepath.Clean(root)}
}

func (z *ZipCache) Root() string {
	if z == nil {
		return ""
	}
	return z.root
}

func (z *ZipCache) Extract(zipPath string) (string, error) {
	if z == nil {
		return "", fmt.Errorf("zip cache is nil")
	}
	digest, err := zipDigest(zipPath)
	if err != nil {
		return "", err
	}
	target := filepath.Join(z.root, digest)
	marker := filepath.Join(target, ".ready")
	if _, err := os.Stat(marker); err == nil {
		return target, nil
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return "", err
	}
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return "", err
	}
	defer r.Close()
	for _, file := range r.File {
		cleanName := filepath.Clean(file.Name)
		if strings.HasPrefix(cleanName, "..") || filepath.IsAbs(cleanName) {
			return "", fmt.Errorf("zip contains unsafe path: %s", file.Name)
		}
		destination := filepath.Join(target, cleanName)
		if !strings.HasPrefix(destination, target+string(os.PathSeparator)) && destination != target {
			return "", fmt.Errorf("zip path escapes target: %s", file.Name)
		}
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(destination, 0o755); err != nil {
				return "", err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return "", err
		}
		src, err := file.Open()
		if err != nil {
			return "", err
		}
		dst, err := os.OpenFile(destination, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			src.Close()
			return "", err
		}
		_, copyErr := io.Copy(dst, src)
		src.Close()
		dstCloseErr := dst.Close()
		if copyErr != nil {
			return "", copyErr
		}
		if dstCloseErr != nil {
			return "", dstCloseErr
		}
		_ = os.Chmod(destination, 0o444)
	}
	if err := os.WriteFile(marker, []byte("ok"), 0o444); err != nil {
		return "", err
	}
	return target, nil
}
