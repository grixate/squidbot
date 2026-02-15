package skills

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/grixate/squidbot/internal/config"
)

type Skill struct {
	Name    string
	Path    string
	Summary string
}

type Discovery struct {
	Skills   []Skill
	Warnings []string
}

func Discover(cfg config.Config) Discovery {
	workspace := config.WorkspacePath(cfg)
	cacheDir := cfg.Skills.CacheDir
	if cacheDir == "" {
		cacheDir = workspace + "/.squidbot/skills-cache"
	}
	index := discoverIndex(context.Background(), workspace, cfg, NewZipCache(cacheDir))
	out := Discovery{
		Skills:   make([]Skill, 0, len(index.Skills)),
		Warnings: append([]string(nil), index.Warnings...),
	}
	for _, descriptor := range index.Skills {
		if !descriptor.Valid {
			continue
		}
		out.Skills = append(out.Skills, Skill{Name: descriptor.Name, Path: descriptor.Path, Summary: descriptor.Summary})
	}
	return out
}

func expandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

func summarizeContent(content string) string {
	return extractDescription(content)
}
