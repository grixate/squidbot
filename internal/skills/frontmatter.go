package skills

import (
	"fmt"
	"strings"
)

type skillFrontMatter struct {
	Name        string
	Description string
	Tags        []string
	Aliases     []string
	Examples    []string
	References  []string
	Tools       []string
	Version     string
}

func splitFrontMatter(content string) (front string, body string, hasFrontMatter bool, err error) {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	if !strings.HasPrefix(normalized, "---\n") {
		return "", normalized, false, nil
	}
	rest := normalized[len("---\n"):]
	idx := strings.Index(rest, "\n---\n")
	if idx < 0 {
		return "", "", false, fmt.Errorf("front matter opening delimiter found without closing delimiter")
	}
	front = strings.TrimSpace(rest[:idx])
	body = strings.TrimLeft(rest[idx+len("\n---\n"):], "\n")
	return front, body, true, nil
}

func parseFrontMatter(content string) (skillFrontMatter, map[string]any, string, bool, error) {
	front, body, has, err := splitFrontMatter(content)
	if err != nil {
		return skillFrontMatter{}, nil, "", false, err
	}
	if !has {
		return skillFrontMatter{}, map[string]any{}, body, false, nil
	}
	fm := skillFrontMatter{}
	extra := map[string]any{}
	lines := strings.Split(front, "\n")
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "-") {
			return skillFrontMatter{}, nil, "", true, fmt.Errorf("unexpected list item without key: %q", line)
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			return skillFrontMatter{}, nil, "", true, fmt.Errorf("invalid front matter line: %q", line)
		}
		key := strings.ToLower(strings.TrimSpace(parts[0]))
		value := strings.TrimSpace(parts[1])
		values := []string{}
		if value == "" {
			for j := i + 1; j < len(lines); j++ {
				next := strings.TrimSpace(lines[j])
				if strings.HasPrefix(next, "-") {
					values = append(values, trimYAMLScalar(strings.TrimSpace(strings.TrimPrefix(next, "-"))))
					i = j
					continue
				}
				if next == "" {
					i = j
					continue
				}
				break
			}
		} else if strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]") {
			trimmed := strings.TrimSpace(value[1 : len(value)-1])
			if trimmed != "" {
				for _, item := range strings.Split(trimmed, ",") {
					values = append(values, trimYAMLScalar(strings.TrimSpace(item)))
				}
			}
		} else {
			values = append(values, trimYAMLScalar(value))
		}
		switch key {
		case "name":
			if len(values) > 0 {
				fm.Name = firstNonEmpty(values)
			}
		case "description":
			if len(values) > 0 {
				fm.Description = firstNonEmpty(values)
			}
		case "tags":
			fm.Tags = append(fm.Tags, values...)
		case "aliases":
			fm.Aliases = append(fm.Aliases, values...)
		case "examples":
			fm.Examples = append(fm.Examples, values...)
		case "references":
			fm.References = append(fm.References, values...)
		case "tools":
			fm.Tools = append(fm.Tools, values...)
		case "version":
			if len(values) > 0 {
				fm.Version = firstNonEmpty(values)
			}
		default:
			if len(values) == 1 {
				extra[key] = values[0]
			} else {
				extra[key] = append([]string(nil), values...)
			}
		}
	}
	return fm, extra, body, true, nil
}

func trimYAMLScalar(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, "\"'")
	return value
}

func firstNonEmpty(values []string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
