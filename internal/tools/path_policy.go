package tools

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

type PathPolicy struct {
	workspace string
}

func NewPathPolicy(workspace string) (*PathPolicy, error) {
	if workspace == "" {
		return nil, errors.New("workspace path required")
	}
	abs, err := filepath.Abs(expandPath(workspace))
	if err != nil {
		return nil, err
	}
	return &PathPolicy{workspace: filepath.Clean(abs)}, nil
}

func (p *PathPolicy) Resolve(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("path required")
	}
	path = expandPath(path)
	if !filepath.IsAbs(path) {
		path = filepath.Join(p.workspace, path)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	clean := filepath.Clean(abs)
	workspaceWithSep := p.workspace + string(os.PathSeparator)
	if clean != p.workspace && !strings.HasPrefix(clean, workspaceWithSep) {
		return "", errors.New("path outside workspace is not allowed")
	}
	return clean, nil
}

func expandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}
