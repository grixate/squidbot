package channels

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/grixate/squidbot/internal/config"
)

func metadataString(cfg config.GenericChannelConfig, key string) string {
	if cfg.Metadata == nil {
		return ""
	}
	return strings.TrimSpace(cfg.Metadata[key])
}

func metadataBool(cfg config.GenericChannelConfig, key string) bool {
	value := strings.ToLower(strings.TrimSpace(metadataString(cfg, key)))
	if value == "" {
		return false
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false
	}
	return parsed
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func startHTTPServer(ctx context.Context, server *http.Server) <-chan error {
	result := make(chan error, 1)
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	go func() {
		err := server.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			result <- err
			return
		}
		result <- nil
	}()
	return result
}

func requireBearerAuth(r *http.Request, token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil
	}
	authz := strings.TrimSpace(r.Header.Get("Authorization"))
	if authz != "Bearer "+token {
		return fmt.Errorf("unauthorized")
	}
	return nil
}
