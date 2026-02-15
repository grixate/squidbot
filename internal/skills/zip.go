package skills

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func readSkillFromZip(path string) ([]byte, error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	matches := make([]*zip.File, 0, 1)
	for _, file := range r.File {
		if file.FileInfo().IsDir() {
			continue
		}
		if strings.EqualFold(filepath.Base(file.Name), "SKILL.md") {
			matches = append(matches, file)
		}
	}
	if len(matches) != 1 {
		return nil, fmt.Errorf("zip skill package %s must contain exactly one SKILL.md (found %d)", path, len(matches))
	}
	fd, err := matches[0].Open()
	if err != nil {
		return nil, err
	}
	defer fd.Close()
	return io.ReadAll(fd)
}

func zipDigest(path string) (string, error) {
	fd, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer fd.Close()
	h := sha256.New()
	if _, err := io.Copy(h, fd); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func findZipFiles(root string) ([]string, error) {
	root = filepath.Clean(root)
	info, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if !info.IsDir() {
		if strings.HasSuffix(strings.ToLower(info.Name()), ".zip") {
			return []string{root}, nil
		}
		return nil, nil
	}
	out := make([]string, 0)
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasSuffix(strings.ToLower(d.Name()), ".zip") {
			out = append(out, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}
