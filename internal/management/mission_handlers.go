package management

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/grixate/squidbot/internal/config"
	"github.com/grixate/squidbot/internal/mission"
)

func (s *Server) withManageAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.sessionFromRequest(r); !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (s *Server) handleManageOverview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	out, err := s.mission.Overview(r.Context())
	if err != nil {
		writeManageError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleManageKanban(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	board, err := s.mission.Kanban(r.Context())
	if err != nil {
		writeManageError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, board)
}

func (s *Server) handleManageKanbanColumns(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Columns []mission.Column `json:"columns"`
	}
	if err := readJSON(r.Body, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	columns, err := s.mission.SetColumns(r.Context(), req.Columns)
	if err != nil {
		writeManageError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"columns": columns})
}

func (s *Server) handleManageKanbanTasks(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		board, err := s.mission.Kanban(r.Context())
		if err != nil {
			writeManageError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"tasks": board.Tasks})
	case http.MethodPost:
		var req struct {
			Title       string             `json:"title"`
			Description string             `json:"description"`
			ColumnID    string             `json:"columnId"`
			Priority    string             `json:"priority"`
			Assignee    string             `json:"assignee"`
			Notes       string             `json:"notes"`
			DueAt       string             `json:"dueAt"`
			Source      mission.TaskSource `json:"source"`
			Dedupe      bool               `json:"dedupe"`
		}
		if err := readJSON(r.Body, &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var dueAt *time.Time
		if strings.TrimSpace(req.DueAt) != "" {
			parsed, err := time.Parse(time.RFC3339, req.DueAt)
			if err != nil {
				http.Error(w, "invalid dueAt (RFC3339 expected)", http.StatusBadRequest)
				return
			}
			parsed = parsed.UTC()
			dueAt = &parsed
		}
		source := req.Source
		if strings.TrimSpace(string(source.Type)) == "" {
			source.Type = mission.TaskSourceManual
		}
		task, deduped, err := s.mission.CreateTask(r.Context(), CreateTaskInput{
			Title:       req.Title,
			Description: req.Description,
			ColumnID:    req.ColumnID,
			Priority:    req.Priority,
			Assignee:    req.Assignee,
			Notes:       req.Notes,
			DueAt:       dueAt,
			Source:      source,
			Dedupe:      req.Dedupe,
		})
		if err != nil {
			writeManageError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"task": task, "deduped": deduped})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleManageKanbanTaskByID(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/manage/kanban/tasks/")
	rest = strings.Trim(strings.TrimSpace(rest), "/")
	if rest == "" {
		http.NotFound(w, r)
		return
	}
	parts := strings.Split(rest, "/")
	taskID := parts[0]
	if len(parts) > 1 && parts[1] == "move" {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			ColumnID string `json:"columnId"`
			Position int    `json:"position"`
		}
		if err := readJSON(r.Body, &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		task, err := s.mission.MoveTask(r.Context(), taskID, req.ColumnID, req.Position)
		if err != nil {
			writeManageError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"task": task})
		return
	}

	switch r.Method {
	case http.MethodPatch:
		var req struct {
			Title       *string `json:"title,omitempty"`
			Description *string `json:"description,omitempty"`
			ColumnID    *string `json:"columnId,omitempty"`
			Priority    *string `json:"priority,omitempty"`
			Assignee    *string `json:"assignee,omitempty"`
			Notes       *string `json:"notes,omitempty"`
			DueAt       *string `json:"dueAt,omitempty"`
			ClearDueAt  bool    `json:"clearDueAt,omitempty"`
		}
		if err := readJSON(r.Body, &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var dueAt *time.Time
		if req.DueAt != nil && strings.TrimSpace(*req.DueAt) != "" {
			parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(*req.DueAt))
			if err != nil {
				http.Error(w, "invalid dueAt (RFC3339 expected)", http.StatusBadRequest)
				return
			}
			parsed = parsed.UTC()
			dueAt = &parsed
		}
		task, err := s.mission.UpdateTask(r.Context(), taskID, UpdateTaskInput{
			Title:       req.Title,
			Description: req.Description,
			ColumnID:    req.ColumnID,
			Priority:    req.Priority,
			Assignee:    req.Assignee,
			Notes:       req.Notes,
			DueAt:       dueAt,
			ClearDueAt:  req.ClearDueAt,
		})
		if err != nil {
			writeManageError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"task": task})
	case http.MethodDelete:
		if err := s.mission.DeleteTask(r.Context(), taskID); err != nil {
			writeManageError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleManageHeartbeat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	out, err := s.mission.HeartbeatStatus(r.Context())
	if err != nil {
		writeManageError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleManageHeartbeatTrigger(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	result, err := s.mission.TriggerHeartbeat(r.Context())
	if err != nil {
		writeManageError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "result": result})
}

func (s *Server) handleManageHeartbeatInterval(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		IntervalSec int `json:"intervalSec"`
	}
	if err := readJSON(r.Body, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	out, err := s.mission.SetHeartbeatInterval(r.Context(), time.Duration(req.IntervalSec)*time.Second)
	if err != nil {
		writeManageError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleManageMemorySearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	limit := 8
	if value := strings.TrimSpace(r.URL.Query().Get("limit")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	chunks, err := s.mission.MemorySearch(r.Context(), query, limit)
	if err != nil {
		writeManageError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": chunks})
}

func (s *Server) handleManageFiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if id := strings.TrimSpace(r.URL.Query().Get("id")); id != "" {
		file, err := s.mission.ReadFile(r.Context(), id)
		if err != nil {
			writeManageError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, file)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": s.mission.ListFiles(r.Context())})
}

func (s *Server) handleManageFileByID(w http.ResponseWriter, r *http.Request) {
	fileID := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/manage/files/"), "/")
	if fileID == "" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPut {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Content string `json:"content"`
		ETag    string `json:"etag"`
	}
	if err := readJSON(r.Body, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	out, err := s.mission.WriteFile(r.Context(), fileID, req.Content, req.ETag)
	if err != nil {
		writeManageError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleManageAnalyticsHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	out, err := s.mission.AnalyticsHealth(r.Context())
	if err != nil {
		writeManageError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleManageAnalyticsLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	limit := 100
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	out, err := s.mission.AnalyticsLogs(r.Context(), limit)
	if err != nil {
		writeManageError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"logs": out})
}

func (s *Server) handleManageSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, s.mission.Settings())
}

func (s *Server) handleManageSettingsProviderTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Provider string `json:"provider"`
		APIKey   string `json:"apiKey"`
		APIBase  string `json:"apiBase"`
		Model    string `json:"model"`
	}
	if err := readJSON(r.Body, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	if err := s.mission.TestProvider(ctx, req.Provider, config.ProviderConfig{
		APIKey:  strings.TrimSpace(req.APIKey),
		APIBase: strings.TrimSpace(req.APIBase),
		Model:   strings.TrimSpace(req.Model),
	}); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleManageSettingsProvider(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Provider string `json:"provider"`
		APIKey   string `json:"apiKey"`
		APIBase  string `json:"apiBase"`
		Model    string `json:"model"`
		Activate bool   `json:"activate"`
		Remove   bool   `json:"remove"`
	}
	if err := readJSON(r.Body, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	out, err := s.mission.UpdateProvider(r.Context(), req.Provider, config.ProviderConfig{
		APIKey:  strings.TrimSpace(req.APIKey),
		APIBase: strings.TrimSpace(req.APIBase),
		Model:   strings.TrimSpace(req.Model),
	}, req.Activate, req.Remove)
	if err != nil {
		writeManageError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleManageSettingsTelegram(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Enabled   bool     `json:"enabled"`
		Token     string   `json:"token"`
		AllowFrom []string `json:"allowFrom"`
	}
	if err := readJSON(r.Body, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	out, err := s.mission.UpdateTelegram(r.Context(), config.TelegramConfig{
		Enabled:   req.Enabled,
		Token:     strings.TrimSpace(req.Token),
		AllowFrom: req.AllowFrom,
	})
	if err != nil {
		writeManageError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleManageSettingsRuntime(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		HeartbeatIntervalSec *int `json:"heartbeatIntervalSec"`
		MailboxSize          *int `json:"mailboxSize"`
	}
	if err := readJSON(r.Body, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	out, err := s.mission.UpdateRuntime(r.Context(), req.HeartbeatIntervalSec, req.MailboxSize)
	if err != nil {
		writeManageError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleManageSettingsPassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if err := readJSON(r.Body, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	out, err := s.mission.UpdatePassword(r.Context(), req.CurrentPassword, req.NewPassword, s.passwordMinLength)
	if err != nil {
		writeManageError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func writeManageError(w http.ResponseWriter, err error) {
	if err == nil {
		http.Error(w, "request failed", http.StatusInternalServerError)
		return
	}
	switch {
	case errors.Is(err, errNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, errConflict):
		http.Error(w, err.Error(), http.StatusConflict)
	default:
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
}
