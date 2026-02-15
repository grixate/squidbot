package skills

import (
	"fmt"
	"path/filepath"
	"strings"
)

func parseSkillDocument(sourcePath, packageRoot, sourceKind string, content []byte) SkillDescriptor {
	text := string(content)
	fm, extra, body, _, err := parseFrontMatter(text)
	desc := SkillDescriptor{
		Path:       sourcePath,
		RootPath:   packageRoot,
		SourceKind: sourceKind,
		Valid:      true,
		Extra:      map[string]any{},
	}
	if err != nil {
		desc.Valid = false
		desc.Errors = append(desc.Errors, err.Error())
	}

	if strings.TrimSpace(fm.Name) != "" {
		desc.Name = strings.TrimSpace(fm.Name)
	}
	if desc.Name == "" {
		desc.Name = extractHeadingName(text)
	}
	if desc.Name == "" {
		desc.Name = filepath.Base(packageRoot)
	}
	desc.ID = normalizeID(desc.Name)

	desc.Description = strings.TrimSpace(fm.Description)
	if desc.Description == "" {
		desc.Description = extractDescription(body)
	}
	desc.Tags = normalizeList(fm.Tags)
	desc.Aliases = normalizeList(fm.Aliases)
	desc.Examples = normalizeList(fm.Examples)
	desc.References = normalizeList(fm.References)
	desc.Tools = normalizeList(fm.Tools)
	desc.Version = strings.TrimSpace(fm.Version)
	desc.Summary = summarizeContent(body)
	if desc.Summary == "" {
		desc.Summary = summarizeContent(text)
	}
	if desc.Summary == "" {
		desc.Summary = "No summary provided."
	}
	if len(extra) > 0 {
		desc.Extra = extra
	}
	if strings.TrimSpace(desc.Description) == "" {
		desc.Description = desc.Summary
	}
	if desc.ID == "" {
		desc.Valid = false
		desc.Errors = append(desc.Errors, fmt.Sprintf("unable to derive skill id from name %q", desc.Name))
	}
	return desc
}

func extractHeadingName(content string) string {
	for _, line := range strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, "# "))
		}
	}
	return ""
}

func extractDescription(content string) string {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
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
	return strings.Join(parts, " ")
}

func normalizeList(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}
