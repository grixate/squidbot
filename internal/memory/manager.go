package memory

import (
	"context"
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"github.com/grixate/squidbot/internal/config"
)

const (
	defaultDailyRetentionDays = 90
	maxChunkChars             = 900
)

type Manager struct {
	enabled            bool
	workspace          string
	indexPath          string
	topK               int
	recencyDays        int
	embeddingsProvider string
	embeddingsModel    string
	mu                 sync.Mutex
}

type Chunk struct {
	ID      string
	Path    string
	Kind    string
	Day     string
	Content string
	Score   float64
}

type DailyEntry struct {
	Time      time.Time
	Source    string
	SessionID string
	Intent    string
	Outcome   string
	FollowUp  bool
}

type sourceDoc struct {
	path      string
	relPath   string
	kind      string
	day       string
	content   string
	updatedAt int64
}

func NewManager(cfg config.Config) *Manager {
	indexPath := strings.TrimSpace(cfg.Memory.IndexPath)
	if indexPath == "" {
		indexPath = filepath.Join(config.DataRoot(), "memory_index.db")
	}
	indexPath = expandPath(indexPath)
	if !filepath.IsAbs(indexPath) {
		indexPath = filepath.Join(config.WorkspacePath(cfg), indexPath)
	}

	topK := cfg.Memory.TopK
	if topK <= 0 {
		topK = 8
	}
	recencyDays := cfg.Memory.RecencyDays
	if recencyDays <= 0 {
		recencyDays = 30
	}

	return &Manager{
		enabled:            cfg.Memory.Enabled,
		workspace:          config.WorkspacePath(cfg),
		indexPath:          filepath.Clean(indexPath),
		topK:               topK,
		recencyDays:        recencyDays,
		embeddingsProvider: strings.TrimSpace(cfg.Memory.EmbeddingsProvider),
		embeddingsModel:    strings.TrimSpace(cfg.Memory.EmbeddingsModel),
	}
}

func (m *Manager) Enabled() bool {
	return m != nil && m.enabled
}

func (m *Manager) EnsureIndex(_ context.Context) error {
	if !m.Enabled() {
		return nil
	}
	db, _, err := m.openDB()
	if err != nil {
		return err
	}
	return db.Close()
}

func (m *Manager) Sync(_ context.Context) error {
	if !m.Enabled() {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	db, ftsEnabled, err := m.openDB()
	if err != nil {
		return err
	}
	defer db.Close()

	sources, err := m.collectSources()
	if err != nil {
		return err
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	seen := map[string]struct{}{}
	for _, source := range sources {
		chunks := chunkContent(source.content, maxChunkChars)
		for idx, chunkText := range chunks {
			chunkID := stableChunkID(source.relPath, idx, chunkText)
			seen[chunkID] = struct{}{}
			if _, err := tx.Exec(
				`INSERT INTO chunks (id, path, kind, day, content, updated_at) VALUES (?, ?, ?, ?, ?, ?)
				ON CONFLICT(id) DO UPDATE SET path=excluded.path, kind=excluded.kind, day=excluded.day, content=excluded.content, updated_at=excluded.updated_at`,
				chunkID, source.path, source.kind, source.day, chunkText, source.updatedAt,
			); err != nil {
				return err
			}
			if ftsEnabled {
				if _, err := tx.Exec(`DELETE FROM chunks_fts WHERE id = ?`, chunkID); err != nil {
					return err
				}
				if _, err := tx.Exec(`INSERT INTO chunks_fts (id, path, kind, day, content) VALUES (?, ?, ?, ?, ?)`, chunkID, source.path, source.kind, source.day, chunkText); err != nil {
					return err
				}
			}
		}
	}

	rows, err := tx.Query(`SELECT id FROM chunks`)
	if err != nil {
		return err
	}
	defer rows.Close()

	toDelete := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		if _, ok := seen[id]; !ok {
			toDelete = append(toDelete, id)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, id := range toDelete {
		if _, err := tx.Exec(`DELETE FROM chunks WHERE id = ?`, id); err != nil {
			return err
		}
		if ftsEnabled {
			if _, err := tx.Exec(`DELETE FROM chunks_fts WHERE id = ?`, id); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(`DELETE FROM embeddings WHERE chunk_id = ?`, id); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (m *Manager) Search(_ context.Context, query string, limit int) ([]Chunk, error) {
	if !m.Enabled() {
		return nil, nil
	}

	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = m.topK
	}

	db, _, err := m.openDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()

	results, err := m.searchFTS(db, query, limit*3)
	if err != nil || len(results) == 0 {
		results, err = m.searchLike(db, query, limit*3)
		if err != nil {
			return nil, err
		}
	}

	m.applyHybridScore(results)
	sort.Slice(results, func(i, j int) bool {
		if results[i].Score == results[j].Score {
			return results[i].ID < results[j].ID
		}
		return results[i].Score > results[j].Score
	})

	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

func (m *Manager) RecentDaily(_ context.Context, limit int) ([]Chunk, error) {
	if !m.Enabled() {
		return nil, nil
	}
	if limit <= 0 {
		limit = 4
	}

	db, _, err := m.openDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()

	cutoff := time.Now().UTC().AddDate(0, 0, -m.recencyDays).Format("2006-01-02")
	rows, err := db.Query(`SELECT id, path, kind, day, content FROM chunks WHERE kind = 'daily' AND day >= ? ORDER BY day DESC, updated_at DESC LIMIT ?`, cutoff, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]Chunk, 0, limit)
	for rows.Next() {
		var c Chunk
		if err := rows.Scan(&c.ID, &c.Path, &c.Kind, &c.Day, &c.Content); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (m *Manager) AppendDailyLog(ctx context.Context, entry DailyEntry) error {
	if !m.Enabled() {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if entry.Time.IsZero() {
		entry.Time = time.Now().UTC()
	}
	if strings.TrimSpace(entry.Source) == "" {
		entry.Source = "conversation"
	}

	dailyDir := filepath.Join(m.workspace, "memory", "daily")
	if err := os.MkdirAll(dailyDir, 0o755); err != nil {
		return err
	}
	day := entry.Time.UTC().Format("2006-01-02")
	dailyPath := filepath.Join(dailyDir, day+".md")

	if _, err := os.Stat(dailyPath); os.IsNotExist(err) {
		header := "# " + day + "\n\n"
		if writeErr := os.WriteFile(dailyPath, []byte(header), 0o644); writeErr != nil {
			return writeErr
		}
	}

	f, err := os.OpenFile(dailyPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	intent := sanitizeInline(entry.Intent, 240)
	outcome := sanitizeInline(entry.Outcome, 320)
	if intent == "" {
		intent = "n/a"
	}
	if outcome == "" {
		outcome = "n/a"
	}
	followUp := "no"
	if entry.FollowUp {
		followUp = "yes"
	}
	if _, err := fmt.Fprintf(
		f,
		"## %s [%s]\n- Session: %s\n- Intent: %s\n- Outcome: %s\n- Follow-up: %s\n\n",
		entry.Time.UTC().Format("15:04:05Z"),
		sanitizeInline(entry.Source, 48),
		sanitizeInline(entry.SessionID, 80),
		intent,
		outcome,
		followUp,
	); err != nil {
		return err
	}

	if err := m.pruneDailyLocked(defaultDailyRetentionDays); err != nil {
		return err
	}
	return m.syncLocked(ctx)
}

func (m *Manager) PruneDaily(_ context.Context, keepDays int) error {
	if !m.Enabled() {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	return m.pruneDailyLocked(keepDays)
}

func (m *Manager) syncLocked(_ context.Context) error {
	db, ftsEnabled, err := m.openDB()
	if err != nil {
		return err
	}
	defer db.Close()

	sources, err := m.collectSources()
	if err != nil {
		return err
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	seen := map[string]struct{}{}
	for _, source := range sources {
		chunks := chunkContent(source.content, maxChunkChars)
		for idx, chunkText := range chunks {
			chunkID := stableChunkID(source.relPath, idx, chunkText)
			seen[chunkID] = struct{}{}
			if _, err := tx.Exec(
				`INSERT INTO chunks (id, path, kind, day, content, updated_at) VALUES (?, ?, ?, ?, ?, ?)
				ON CONFLICT(id) DO UPDATE SET path=excluded.path, kind=excluded.kind, day=excluded.day, content=excluded.content, updated_at=excluded.updated_at`,
				chunkID, source.path, source.kind, source.day, chunkText, source.updatedAt,
			); err != nil {
				return err
			}
			if ftsEnabled {
				if _, err := tx.Exec(`DELETE FROM chunks_fts WHERE id = ?`, chunkID); err != nil {
					return err
				}
				if _, err := tx.Exec(`INSERT INTO chunks_fts (id, path, kind, day, content) VALUES (?, ?, ?, ?, ?)`, chunkID, source.path, source.kind, source.day, chunkText); err != nil {
					return err
				}
			}
		}
	}

	rows, err := tx.Query(`SELECT id FROM chunks`)
	if err != nil {
		return err
	}
	defer rows.Close()
	toDelete := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		if _, ok := seen[id]; !ok {
			toDelete = append(toDelete, id)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range toDelete {
		if _, err := tx.Exec(`DELETE FROM chunks WHERE id = ?`, id); err != nil {
			return err
		}
		if ftsEnabled {
			if _, err := tx.Exec(`DELETE FROM chunks_fts WHERE id = ?`, id); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(`DELETE FROM embeddings WHERE chunk_id = ?`, id); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (m *Manager) pruneDailyLocked(keepDays int) error {
	if keepDays <= 0 {
		keepDays = defaultDailyRetentionDays
	}
	dailyDir := filepath.Join(m.workspace, "memory", "daily")
	entries, err := os.ReadDir(dailyDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -keepDays)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".md") {
			continue
		}
		day := strings.TrimSuffix(name, ".md")
		parsed, err := time.Parse("2006-01-02", day)
		if err != nil {
			continue
		}
		if parsed.Before(cutoff) {
			_ = os.Remove(filepath.Join(dailyDir, name))
		}
	}
	return nil
}

func (m *Manager) openDB() (*sql.DB, bool, error) {
	if err := os.MkdirAll(filepath.Dir(m.indexPath), 0o755); err != nil {
		return nil, false, err
	}
	db, err := sql.Open("sqlite", m.indexPath)
	if err != nil {
		return nil, false, err
	}
	if _, err := db.Exec(`PRAGMA journal_mode = WAL;`); err != nil {
		_ = db.Close()
		return nil, false, err
	}
	ftsEnabled, err := ensureSchema(db)
	if err != nil {
		_ = db.Close()
		return nil, false, err
	}
	return db, ftsEnabled, nil
}

func ensureSchema(db *sql.DB) (bool, error) {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS chunks (
		id TEXT PRIMARY KEY,
		path TEXT NOT NULL,
		kind TEXT NOT NULL,
		day TEXT,
		content TEXT NOT NULL,
		updated_at INTEGER NOT NULL,
		created_at INTEGER NOT NULL DEFAULT (unixepoch())
	)`); err != nil {
		return false, err
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_chunks_kind_day ON chunks(kind, day)`); err != nil {
		return false, err
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS embeddings (
		chunk_id TEXT PRIMARY KEY,
		provider TEXT NOT NULL,
		model TEXT NOT NULL,
		vector BLOB NOT NULL,
		updated_at INTEGER NOT NULL
	)`); err != nil {
		return false, err
	}

	if _, err := db.Exec(`CREATE VIRTUAL TABLE IF NOT EXISTS chunks_fts USING fts5(id UNINDEXED, path UNINDEXED, kind UNINDEXED, day UNINDEXED, content)`); err != nil {
		return false, nil
	}
	return true, nil
}

func (m *Manager) collectSources() ([]sourceDoc, error) {
	workspace := m.workspace
	sources := make([]sourceDoc, 0, 32)

	curatedPath := filepath.Join(workspace, "memory", "MEMORY.md")
	if content, updatedAt, err := readSource(curatedPath); err == nil && strings.TrimSpace(content) != "" {
		sources = append(sources, sourceDoc{
			path:      curatedPath,
			relPath:   filepath.ToSlash(strings.TrimPrefix(curatedPath, workspace+string(os.PathSeparator))),
			kind:      "curated",
			day:       "",
			content:   content,
			updatedAt: updatedAt,
		})
	}

	dailyMatches, err := filepath.Glob(filepath.Join(workspace, "memory", "daily", "*.md"))
	if err != nil {
		return nil, err
	}
	sort.Strings(dailyMatches)
	for _, p := range dailyMatches {
		content, updatedAt, err := readSource(p)
		if err != nil || strings.TrimSpace(content) == "" {
			continue
		}
		day := strings.TrimSuffix(filepath.Base(p), ".md")
		if _, err := time.Parse("2006-01-02", day); err != nil {
			day = ""
		}
		sources = append(sources, sourceDoc{
			path:      p,
			relPath:   filepath.ToSlash(strings.TrimPrefix(p, workspace+string(os.PathSeparator))),
			kind:      "daily",
			day:       day,
			content:   content,
			updatedAt: updatedAt,
		})
	}

	return sources, nil
}

func (m *Manager) searchFTS(db *sql.DB, query string, limit int) ([]Chunk, error) {
	if limit <= 0 {
		limit = m.topK
	}
	q := buildFTSQuery(query)
	if q == "" {
		return nil, nil
	}
	rows, err := db.Query(
		`SELECT c.id, c.path, c.kind, c.day, c.content, bm25(chunks_fts) AS lexical
		FROM chunks_fts
		JOIN chunks c ON c.id = chunks_fts.id
		WHERE chunks_fts MATCH ?
		LIMIT ?`,
		q,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]Chunk, 0, limit)
	for rows.Next() {
		var c Chunk
		var lexical float64
		if err := rows.Scan(&c.ID, &c.Path, &c.Kind, &c.Day, &c.Content, &lexical); err != nil {
			return nil, err
		}
		c.Score = -lexical
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (m *Manager) searchLike(db *sql.DB, query string, limit int) ([]Chunk, error) {
	if limit <= 0 {
		limit = m.topK
	}
	rows, err := db.Query(
		`SELECT id, path, kind, day, content
		FROM chunks
		WHERE content LIKE '%' || ? || '%'
		ORDER BY updated_at DESC
		LIMIT ?`,
		query,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]Chunk, 0, limit)
	for rows.Next() {
		var c Chunk
		if err := rows.Scan(&c.ID, &c.Path, &c.Kind, &c.Day, &c.Content); err != nil {
			return nil, err
		}
		c.Score = 0
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (m *Manager) applyHybridScore(chunks []Chunk) {
	now := time.Now().UTC()
	for idx := range chunks {
		lexical := chunks[idx].Score
		vector := 0.0
		if strings.TrimSpace(m.embeddingsProvider) != "" && strings.ToLower(strings.TrimSpace(m.embeddingsProvider)) != "none" {
			vector = 0 // Embeddings are optional; lexical search remains the fallback baseline.
		}
		recencyBoost := 0.0
		if chunks[idx].Day != "" {
			if day, err := time.Parse("2006-01-02", chunks[idx].Day); err == nil {
				ageDays := int(now.Sub(day).Hours() / 24)
				if ageDays >= 0 && ageDays <= m.recencyDays {
					recencyBoost = 0.35
				}
			}
		}
		chunks[idx].Score = lexical + vector + recencyBoost
	}
}

func readSource(path string) (string, int64, error) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return "", 0, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", 0, err
	}
	return string(bytes), info.ModTime().UTC().Unix(), nil
}

func chunkContent(content string, chunkLimit int) []string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	paragraphs := splitParagraphs(content)
	if len(paragraphs) == 0 {
		return nil
	}

	out := make([]string, 0, len(paragraphs))
	var current strings.Builder
	for _, paragraph := range paragraphs {
		if current.Len() > 0 && current.Len()+2+len(paragraph) > chunkLimit {
			out = append(out, current.String())
			current.Reset()
		}
		if current.Len() > 0 {
			current.WriteString("\n\n")
		}
		if len(paragraph) > chunkLimit {
			for len(paragraph) > chunkLimit {
				out = append(out, paragraph[:chunkLimit])
				paragraph = paragraph[chunkLimit:]
			}
			paragraph = strings.TrimSpace(paragraph)
			if paragraph == "" {
				continue
			}
		}
		current.WriteString(paragraph)
	}
	if current.Len() > 0 {
		out = append(out, current.String())
	}
	return out
}

func splitParagraphs(content string) []string {
	parts := strings.Split(content, "\n\n")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}

func stableChunkID(relPath string, index int, content string) string {
	h := sha1.New()
	_, _ = h.Write([]byte(relPath))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(fmt.Sprintf("%d", index)))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(content))
	return hex.EncodeToString(h.Sum(nil))
}

func sanitizeInline(value string, maxLen int) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\n", " "))
	value = strings.Join(strings.Fields(value), " ")
	if maxLen > 0 && len(value) > maxLen {
		return value[:maxLen-3] + "..."
	}
	return value
}

func buildFTSQuery(query string) string {
	tokens := strings.Fields(strings.ToLower(query))
	if len(tokens) == 0 {
		return ""
	}
	parts := make([]string, 0, len(tokens))
	for _, token := range tokens {
		clean := strings.Trim(token, "\"'()[]{}:;,.!?")
		if clean == "" {
			continue
		}
		parts = append(parts, `"`+clean+`"*`)
	}
	return strings.Join(parts, " ")
}

func expandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}
