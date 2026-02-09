package skills

import (
	"os"
	"path/filepath"
	"sort"
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
	roots := resolveRoots(workspace, cfg.Skills.Paths)
	result := Discovery{
		Skills:   []Skill{},
		Warnings: []string{},
	}

	for _, root := range roots {
		info, err := os.Stat(root)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			result.Warnings = append(result.Warnings, "skills: failed to stat "+root+": "+err.Error())
			continue
		}
		if !info.IsDir() {
			result.Warnings = append(result.Warnings, "skills: path is not a directory: "+root)
			continue
		}

		walkErr := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
			if walkErr != nil {
				result.Warnings = append(result.Warnings, "skills: failed to read "+path+": "+walkErr.Error())
				return nil
			}
			if d.IsDir() || d.Name() != "SKILL.md" {
				return nil
			}

			content, readErr := os.ReadFile(path)
			if readErr != nil {
				result.Warnings = append(result.Warnings, "skills: failed to read "+path+": "+readErr.Error())
				return nil
			}
			skill := Skill{
				Name:    parseName(path, string(content)),
				Path:    path,
				Summary: summarizeContent(string(content)),
			}
			if strings.TrimSpace(skill.Name) == "" {
				skill.Name = filepath.Base(filepath.Dir(path))
			}
			if strings.TrimSpace(skill.Summary) == "" {
				skill.Summary = "No summary provided."
			}
			result.Skills = append(result.Skills, skill)
			return nil
		})
		if walkErr != nil {
			result.Warnings = append(result.Warnings, "skills: failed to walk "+root+": "+walkErr.Error())
		}
	}

	sort.Slice(result.Skills, func(i, j int) bool {
		return result.Skills[i].Path < result.Skills[j].Path
	})
	return result
}

func resolveRoots(workspace string, configured []string) []string {
	paths := configured
	if len(paths) == 0 {
		paths = []string{filepath.Join(workspace, "skills")}
	}

	out := make([]string, 0, len(paths))
	seen := map[string]struct{}{}
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		path = expandPath(path)
		if !filepath.IsAbs(path) {
			path = filepath.Join(workspace, path)
		}
		path = filepath.Clean(path)
		if _, exists := seen[path]; exists {
			continue
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}

func parseName(path, content string) string {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, "# "))
		}
	}
	return filepath.Base(filepath.Dir(path))
}

func summarizeContent(content string) string {
	lines := strings.Split(content, "\n")
	parts := make([]string, 0, 2)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		trimmed = strings.TrimPrefix(trimmed, "- ")
		trimmed = strings.TrimPrefix(trimmed, "* ")
		parts = append(parts, trimmed)
		if len(parts) == 2 {
			break
		}
	}
	summary := strings.Join(parts, " ")
	if len(summary) > 260 {
		return summary[:257] + "..."
	}
	return summary
}

func expandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}
