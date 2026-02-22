package secrets

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const secretRefPrefix = "secretRef:"

type Resolver struct{}

func NewResolver() *Resolver {
	return &Resolver{}
}

func (r *Resolver) Resolve(ref string) (string, error) {
	trimmed := strings.TrimSpace(ref)
	if trimmed == "" {
		return "", nil
	}
	parts := strings.SplitN(trimmed, ":", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid secret ref %q", sanitizeRef(trimmed))
	}
	scheme := strings.ToLower(strings.TrimSpace(parts[0]))
	target := strings.TrimSpace(parts[1])
	if target == "" {
		return "", fmt.Errorf("invalid secret ref %q", sanitizeRef(trimmed))
	}
	switch scheme {
	case "env":
		value := strings.TrimSpace(os.Getenv(target))
		if value == "" {
			return "", fmt.Errorf("env secret %q is empty", target)
		}
		return value, nil
	case "file":
		if !filepath.IsAbs(target) {
			return "", fmt.Errorf("file secret path must be absolute")
		}
		if err := ValidateSecretFile(target); err != nil {
			return "", err
		}
		raw, err := os.ReadFile(target)
		if err != nil {
			return "", err
		}
		value := strings.TrimSpace(string(raw))
		if value == "" {
			return "", fmt.Errorf("file secret %q is empty", target)
		}
		return value, nil
	case "systemd":
		if strings.Contains(target, "/") {
			return "", fmt.Errorf("invalid systemd secret name")
		}
		credDir := strings.TrimSpace(os.Getenv("CREDENTIALS_DIRECTORY"))
		if credDir == "" {
			return "", fmt.Errorf("CREDENTIALS_DIRECTORY is not set")
		}
		path := filepath.Join(credDir, target)
		if runtime.GOOS != "windows" {
			if err := ValidateSecretFile(path); err != nil {
				return "", err
			}
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		value := strings.TrimSpace(string(raw))
		if value == "" {
			return "", fmt.Errorf("systemd secret %q is empty", target)
		}
		return value, nil
	default:
		return "", fmt.Errorf("unsupported secret ref scheme %q", scheme)
	}
}

func (r *Resolver) ResolveMap(in map[string]string) (map[string]string, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		k := strings.TrimSpace(key)
		if k == "" {
			continue
		}
		trimmed := strings.TrimSpace(value)
		if strings.HasPrefix(trimmed, secretRefPrefix) {
			resolved, err := r.Resolve(strings.TrimPrefix(trimmed, secretRefPrefix))
			if err != nil {
				return nil, fmt.Errorf("resolve %s: %w", k, err)
			}
			out[k] = resolved
			continue
		}
		out[k] = value
	}
	return out, nil
}

func IsSecretRefValue(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), secretRefPrefix)
}

func SecretRefValue(ref string) string {
	trimmed := strings.TrimSpace(ref)
	if trimmed == "" {
		return ""
	}
	return secretRefPrefix + trimmed
}

func sanitizeRef(ref string) string {
	parts := strings.SplitN(ref, ":", 2)
	if len(parts) != 2 {
		return "invalid"
	}
	return strings.ToLower(strings.TrimSpace(parts[0])) + ":***"
}
