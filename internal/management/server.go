package management

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/grixate/squidbot/internal/catalog"
	"github.com/grixate/squidbot/internal/config"
	"github.com/grixate/squidbot/internal/provider"
	"github.com/grixate/squidbot/internal/telemetry"
)

const (
	defaultSetupTokenTTL  = 15 * time.Minute
	defaultSessionIdleTTL = 24 * time.Hour
	defaultSessionMaxTTL  = 7 * 24 * time.Hour
	sessionCookieName     = "squidbot_session"
)

type Options struct {
	ConfigPath        string
	RequireSetupToken bool
	SetupTokenTTL     time.Duration
	PasswordMinLength int
	SessionIdleTTL    time.Duration
	SessionMaxTTL     time.Duration
	Runtime           *RuntimeBindings
	Logger            *log.Logger
}

type Server struct {
	mu sync.RWMutex

	cfg        config.Config
	configPath string
	logger     *log.Logger

	requireSetupToken bool
	setupTokenTTL     time.Duration
	passwordMinLength int
	sessionIdleTTL    time.Duration
	sessionMaxTTL     time.Duration

	setupToken      string
	setupTokenUntil time.Time
	setupDone       chan struct{}
	setupDoneOnce   sync.Once

	sessions map[string]sessionRecord

	mission *MissionControlService
}

type sessionRecord struct {
	ID        string
	CreatedAt time.Time
	LastSeen  time.Time
}

func NewServer(cfg config.Config, opts Options) (*Server, error) {
	logger := opts.Logger
	if logger == nil {
		logger = log.New(os.Stderr, "", log.LstdFlags)
	}
	ttl := opts.SetupTokenTTL
	if ttl <= 0 {
		ttl = defaultSetupTokenTTL
	}
	idleTTL := opts.SessionIdleTTL
	if idleTTL <= 0 {
		idleTTL = defaultSessionIdleTTL
	}
	maxTTL := opts.SessionMaxTTL
	if maxTTL <= 0 {
		maxTTL = defaultSessionMaxTTL
	}
	minPassword := opts.PasswordMinLength
	if minPassword <= 0 {
		minPassword = 12
	}

	configPath := strings.TrimSpace(opts.ConfigPath)
	if configPath == "" {
		configPath = config.ConfigPath()
	}

	s := &Server{
		cfg:               cfg,
		configPath:        configPath,
		logger:            logger,
		requireSetupToken: opts.RequireSetupToken,
		setupTokenTTL:     ttl,
		passwordMinLength: minPassword,
		sessionIdleTTL:    idleTTL,
		sessionMaxTTL:     maxTTL,
		setupDone:         make(chan struct{}),
		sessions:          map[string]sessionRecord{},
	}

	missionSvc, err := NewMissionControlService(
		cfg,
		configPath,
		func() *telemetry.Metrics {
			if opts.Runtime != nil && opts.Runtime.Metrics != nil {
				return opts.Runtime.Metrics
			}
			return nil
		}(),
		func() HeartbeatRuntime {
			if opts.Runtime != nil {
				return opts.Runtime.Heartbeat
			}
			return nil
		}(),
		logger,
		func() config.Config {
			s.mu.RLock()
			defer s.mu.RUnlock()
			return s.cfg
		},
		func(next config.Config) error {
			if err := config.Save(configPath, next); err != nil {
				return err
			}
			s.mu.Lock()
			s.cfg = next
			s.mu.Unlock()
			return nil
		},
		func(ctx context.Context, providerName string, providerCfg config.ProviderConfig) error {
			return s.liveProviderCheck(ctx, providerName, providerCfg)
		},
	)
	if err != nil {
		return nil, err
	}
	s.mission = missionSvc

	if config.IsSetupComplete(cfg) {
		s.setupDoneOnce.Do(func() { close(s.setupDone) })
		return s, nil
	}
	if s.requireSetupToken {
		token, err := randomToken(24)
		if err != nil {
			return nil, err
		}
		s.setupToken = token
		s.setupTokenUntil = time.Now().UTC().Add(s.setupTokenTTL)
	}
	return s, nil
}

func (s *Server) LocalBaseURL() string {
	s.mu.RLock()
	host := strings.TrimSpace(s.cfg.Management.Host)
	port := s.cfg.Management.Port
	s.mu.RUnlock()

	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return fmt.Sprintf("http://%s:%d", host, port)
}

func (s *Server) PublicBaseURL() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return strings.TrimRight(strings.TrimSpace(s.cfg.Management.PublicBaseURL), "/")
}

func (s *Server) SetupToken() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.setupToken
}

func (s *Server) SetupCompleted() <-chan struct{} {
	return s.setupDone
}

func (s *Server) Start(ctx context.Context) error {
	defer func() {
		if s.mission != nil {
			_ = s.mission.Close()
		}
	}()

	s.mu.RLock()
	addr := fmt.Sprintf("%s:%d", s.cfg.Management.Host, s.cfg.Management.Port)
	s.mu.RUnlock()

	httpServer := &http.Server{
		Addr:    addr,
		Handler: s.routes(),
	}

	errCh := make(chan error, 1)
	go func() {
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
		<-errCh
		return nil
	case err := <-errCh:
		return err
	}
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/setup/state", s.handleSetupState)
	mux.HandleFunc("/api/setup/provider/test", s.handleSetupProviderTest)
	mux.HandleFunc("/api/setup/complete", s.handleSetupComplete)
	mux.HandleFunc("/api/auth/login", s.handleAuthLogin)
	mux.HandleFunc("/api/auth/logout", s.handleAuthLogout)
	mux.HandleFunc("/api/auth/session", s.handleAuthSession)

	mux.HandleFunc("/api/manage/overview", s.withManageAuth(s.handleManageOverview))
	mux.HandleFunc("/api/manage/kanban", s.withManageAuth(s.handleManageKanban))
	mux.HandleFunc("/api/manage/kanban/columns", s.withManageAuth(s.handleManageKanbanColumns))
	mux.HandleFunc("/api/manage/kanban/tasks", s.withManageAuth(s.handleManageKanbanTasks))
	mux.HandleFunc("/api/manage/kanban/tasks/", s.withManageAuth(s.handleManageKanbanTaskByID))
	mux.HandleFunc("/api/manage/heartbeat", s.withManageAuth(s.handleManageHeartbeat))
	mux.HandleFunc("/api/manage/heartbeat/trigger", s.withManageAuth(s.handleManageHeartbeatTrigger))
	mux.HandleFunc("/api/manage/heartbeat/interval", s.withManageAuth(s.handleManageHeartbeatInterval))
	mux.HandleFunc("/api/manage/memory/search", s.withManageAuth(s.handleManageMemorySearch))
	mux.HandleFunc("/api/manage/files", s.withManageAuth(s.handleManageFiles))
	mux.HandleFunc("/api/manage/files/", s.withManageAuth(s.handleManageFileByID))
	mux.HandleFunc("/api/manage/analytics/health", s.withManageAuth(s.handleManageAnalyticsHealth))
	mux.HandleFunc("/api/manage/analytics/logs", s.withManageAuth(s.handleManageAnalyticsLogs))
	mux.HandleFunc("/api/manage/settings", s.withManageAuth(s.handleManageSettings))
	mux.HandleFunc("/api/manage/settings/provider/test", s.withManageAuth(s.handleManageSettingsProviderTest))
	mux.HandleFunc("/api/manage/settings/provider", s.withManageAuth(s.handleManageSettingsProvider))
	mux.HandleFunc("/api/manage/settings/channels/", s.withManageAuth(s.handleManageSettingsChannel))
	mux.HandleFunc("/api/manage/settings/runtime", s.withManageAuth(s.handleManageSettingsRuntime))
	mux.HandleFunc("/api/manage/settings/password", s.withManageAuth(s.handleManageSettingsPassword))

	mux.HandleFunc("/api/manage/placeholder", s.handleManagePlaceholder)
	mux.HandleFunc("/", s.handleUI)

	return withJSON(mux)
}

func (s *Server) handleSetupState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.mu.RLock()
	cfg := s.cfg
	tokenRequired := s.requireSetupToken && !config.IsSetupComplete(cfg)
	setupComplete := config.IsSetupComplete(cfg)
	tokenUntil := s.setupTokenUntil
	s.mu.RUnlock()

	type providerInfo struct {
		ID             string `json:"id"`
		Label          string `json:"label"`
		RequiresAPIKey bool   `json:"requiresApiKey"`
		RequiresModel  bool   `json:"requiresModel"`
		DefaultAPIBase string `json:"defaultApiBase,omitempty"`
		DefaultModel   string `json:"defaultModel,omitempty"`
	}
	providers := make([]providerInfo, 0, len(config.SupportedProviders()))
	for _, providerName := range config.SupportedProviders() {
		requiresAPIKey, requiresModel, _ := config.ProviderRequirements(providerName)
		providers = append(providers, providerInfo{
			ID:             providerName,
			Label:          providerLabel(providerName),
			RequiresAPIKey: requiresAPIKey,
			RequiresModel:  requiresModel,
			DefaultAPIBase: config.ProviderDefaultAPIBase(providerName),
			DefaultModel:   config.ProviderDefaultModel(providerName),
		})
	}

	currentProvider := strings.TrimSpace(cfg.Providers.Active)
	currentProviderCfg, _ := cfg.ProviderByName(currentProvider)
	channels := make([]map[string]any, 0, len(config.SupportedChannels()))
	currentChannels := map[string]any{}
	for _, channelID := range config.SupportedChannels() {
		profile, _ := config.ChannelProfile(channelID)
		channels = append(channels, map[string]any{
			"id":    channelID,
			"label": profile.Label,
			"kind":  profile.Kind,
		})
		current := cfg.Channels.Registry[channelID]
		currentChannels[channelID] = map[string]any{
			"enabled":   current.Enabled,
			"tokenSet":  strings.TrimSpace(current.Token) != "",
			"allowFrom": current.AllowFrom,
			"endpoint":  current.Endpoint,
			"authSet":   strings.TrimSpace(current.AuthToken) != "",
		}
	}
	if _, ok := currentChannels["telegram"]; !ok {
		currentChannels["telegram"] = map[string]any{
			"enabled":   cfg.Channels.Telegram.Enabled,
			"tokenSet":  strings.TrimSpace(cfg.Channels.Telegram.Token) != "",
			"allowFrom": cfg.Channels.Telegram.AllowFrom,
			"endpoint":  "",
			"authSet":   false,
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"setupComplete":       setupComplete,
		"requiresSetupToken":  tokenRequired,
		"setupTokenExpiresAt": tokenUntil.Format(time.RFC3339),
		"providers":           providers,
		"channels":            channels,
		"current": map[string]any{
			"provider": map[string]any{
				"id":        currentProvider,
				"apiBase":   currentProviderCfg.APIBase,
				"model":     currentProviderCfg.Model,
				"hasApiKey": strings.TrimSpace(currentProviderCfg.APIKey) != "",
			},
			"channels": currentChannels,
		},
	})
}

func (s *Server) handleSetupProviderTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		SetupToken string `json:"setupToken"`
		Provider   string `json:"provider"`
		APIKey     string `json:"apiKey"`
		APIBase    string `json:"apiBase"`
		Model      string `json:"model"`
	}
	if err := readJSON(r.Body, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.requireValidSetupToken(req.SetupToken); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}

	providerCfg := config.ProviderConfig{
		APIKey:  strings.TrimSpace(req.APIKey),
		APIBase: strings.TrimSpace(req.APIBase),
		Model:   strings.TrimSpace(req.Model),
	}
	if err := config.ValidateProviderDraft(req.Provider, providerCfg); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	if err := s.liveProviderCheck(ctx, req.Provider, providerCfg); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleSetupComplete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		SetupToken string `json:"setupToken"`
		Provider   string `json:"provider"`
		APIKey     string `json:"apiKey"`
		APIBase    string `json:"apiBase"`
		Model      string `json:"model"`
		Channel    struct {
			ID        string   `json:"id"`
			Enabled   bool     `json:"enabled"`
			Token     string   `json:"token"`
			AllowFrom []string `json:"allowFrom"`
			Endpoint  string   `json:"endpoint"`
			AuthToken string   `json:"authToken"`
		} `json:"channel"`
		Channels []struct {
			ID        string            `json:"id"`
			Enabled   bool              `json:"enabled"`
			Token     string            `json:"token"`
			AllowFrom []string          `json:"allowFrom"`
			Endpoint  string            `json:"endpoint"`
			AuthToken string            `json:"authToken"`
			Headers   map[string]string `json:"headers"`
			Metadata  map[string]string `json:"metadata"`
		} `json:"channels"`
		Password string `json:"password"`
	}
	if err := readJSON(r.Body, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.requireValidSetupToken(req.SetupToken); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	password := strings.TrimSpace(req.Password)
	if len(password) < s.passwordMinLength {
		http.Error(w, fmt.Sprintf("password must be at least %d characters", s.passwordMinLength), http.StatusBadRequest)
		return
	}
	nextPasswordHash, err := HashPassword(password)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.mu.Lock()
	cfg := s.cfg
	s.mu.Unlock()

	channelConfigs := map[string]config.GenericChannelConfig{}
	for _, channel := range req.Channels {
		channelID := strings.ToLower(strings.TrimSpace(channel.ID))
		if channelID == "" {
			continue
		}
		channelConfigs[channelID] = config.GenericChannelConfig{
			Enabled:   channel.Enabled,
			Token:     strings.TrimSpace(channel.Token),
			AllowFrom: channel.AllowFrom,
			Endpoint:  strings.TrimSpace(channel.Endpoint),
			AuthToken: strings.TrimSpace(channel.AuthToken),
			Headers:   channel.Headers,
			Metadata:  channel.Metadata,
		}
	}
	legacyChannelID := strings.ToLower(strings.TrimSpace(req.Channel.ID))
	if legacyChannelID == "" {
		legacyChannelID = "telegram"
	}
	if len(channelConfigs) == 0 {
		channelConfigs[legacyChannelID] = config.GenericChannelConfig{
			Enabled:   req.Channel.Enabled,
			Token:     strings.TrimSpace(req.Channel.Token),
			AllowFrom: req.Channel.AllowFrom,
			Endpoint:  strings.TrimSpace(req.Channel.Endpoint),
			AuthToken: strings.TrimSpace(req.Channel.AuthToken),
		}
	}
	telegramChannel := channelConfigs["telegram"]
	telegramCfg := config.TelegramConfig{
		Enabled:   telegramChannel.Enabled,
		Token:     strings.TrimSpace(telegramChannel.Token),
		AllowFrom: telegramChannel.AllowFrom,
	}

	nextCfg, err := config.ApplyOnboardingInput(cfg, config.OnboardingInput{
		Provider: req.Provider,
		ProviderConfig: config.ProviderConfig{
			APIKey:  strings.TrimSpace(req.APIKey),
			APIBase: strings.TrimSpace(req.APIBase),
			Model:   strings.TrimSpace(req.Model),
		},
		Telegram: telegramCfg,
		Channels: channelConfigs,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	nextCfg.Auth.PasswordHash = nextPasswordHash
	nextCfg.Auth.PasswordUpdatedAt = time.Now().UTC().Format(time.RFC3339)

	if err := config.Save(s.configPath, nextCfg); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := config.EnsureFilesystem(nextCfg); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.mu.Lock()
	s.cfg = nextCfg
	s.setupToken = ""
	s.setupTokenUntil = time.Time{}
	s.mu.Unlock()
	s.setupDoneOnce.Do(func() { close(s.setupDone) })

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if err := readJSON(r.Body, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	s.mu.RLock()
	cfg := s.cfg
	s.mu.RUnlock()
	if !config.IsSetupComplete(cfg) {
		http.Error(w, "setup is incomplete", http.StatusConflict)
		return
	}
	if !VerifyPassword(strings.TrimSpace(req.Password), cfg.Auth.PasswordHash) {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}

	token, err := randomToken(24)
	if err != nil {
		http.Error(w, "failed to create session", http.StatusInternalServerError)
		return
	}
	now := time.Now().UTC()
	s.mu.Lock()
	s.sessions[token] = sessionRecord{
		ID:        token,
		CreatedAt: now,
		LastSeen:  now,
	}
	s.mu.Unlock()

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Path:     "/",
		MaxAge:   int(s.sessionMaxTTL.Seconds()),
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	cookie, _ := r.Cookie(sessionCookieName)
	if cookie != nil && strings.TrimSpace(cookie.Value) != "" {
		s.mu.Lock()
		delete(s.sessions, cookie.Value)
		s.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Path:     "/",
		MaxAge:   -1,
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleAuthSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	authenticated := false
	expiresAt := ""
	if _, ok := s.sessionFromRequest(r); ok {
		authenticated = true
		expiresAt = time.Now().UTC().Add(s.sessionIdleTTL).Format(time.RFC3339)
	}

	s.mu.RLock()
	setupComplete := config.IsSetupComplete(s.cfg)
	s.mu.RUnlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated": authenticated,
		"setupComplete": setupComplete,
		"expiresAt":     expiresAt,
	})
}

func (s *Server) handleManagePlaceholder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.sessionFromRequest(r); !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"message": "Management features are coming in the next iteration.",
	})
}

func (s *Server) handleUI(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		http.NotFound(w, r)
		return
	}
	clean := path.Clean(r.URL.Path)
	if clean == "." || clean == "/" {
		serveAsset(w, "ui/dist/index.html")
		return
	}
	assetPath := "ui/dist" + clean
	if !assetExists(assetPath) {
		serveAsset(w, "ui/dist/index.html")
		return
	}
	serveAsset(w, assetPath)
}

func (s *Server) requireValidSetupToken(raw string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if config.IsSetupComplete(s.cfg) {
		return fmt.Errorf("setup already completed")
	}
	if !s.requireSetupToken {
		return nil
	}
	token := strings.TrimSpace(raw)
	if token == "" {
		return fmt.Errorf("setup token required")
	}
	if s.setupToken == "" {
		return fmt.Errorf("setup token unavailable")
	}
	if time.Now().UTC().After(s.setupTokenUntil) {
		return fmt.Errorf("setup token expired")
	}
	if token != s.setupToken {
		return fmt.Errorf("invalid setup token")
	}
	return nil
}

func (s *Server) liveProviderCheck(ctx context.Context, providerName string, providerCfg config.ProviderConfig) error {
	normalized, ok := config.NormalizeProviderName(providerName)
	if !ok {
		return fmt.Errorf("unsupported provider %q", providerName)
	}
	if strings.TrimSpace(providerCfg.APIBase) == "" {
		providerCfg.APIBase = config.ProviderDefaultAPIBase(normalized)
	}
	if strings.TrimSpace(providerCfg.Model) == "" {
		if model := config.ProviderDefaultModel(normalized); model != "" {
			providerCfg.Model = model
		}
	}

	s.mu.RLock()
	cfg := s.cfg
	s.mu.RUnlock()
	cfg.Providers.Active = normalized
	_ = cfg.SetProviderByName(normalized, providerCfg)
	client, model, err := provider.FromConfig(cfg)
	if err != nil {
		return err
	}
	_, err = client.Chat(ctx, provider.ChatRequest{
		Messages: []provider.Message{{Role: "user", Content: "Reply with OK"}},
		Model:    model,
		MaxTokens: func() int {
			if cfg.Agents.Defaults.MaxTokens > 8 {
				return 8
			}
			if cfg.Agents.Defaults.MaxTokens <= 0 {
				return 8
			}
			return cfg.Agents.Defaults.MaxTokens
		}(),
		Temperature: 0,
	})
	if err != nil {
		return err
	}
	return nil
}

func (s *Server) sessionFromRequest(r *http.Request) (sessionRecord, bool) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		return sessionRecord{}, false
	}
	id := cookie.Value
	now := time.Now().UTC()

	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.sessions[id]
	if !ok {
		return sessionRecord{}, false
	}
	if now.Sub(rec.LastSeen) > s.sessionIdleTTL {
		delete(s.sessions, id)
		return sessionRecord{}, false
	}
	if now.Sub(rec.CreatedAt) > s.sessionMaxTTL {
		delete(s.sessions, id)
		return sessionRecord{}, false
	}
	rec.LastSeen = now
	s.sessions[id] = rec
	return rec, true
}

func providerLabel(name string) string {
	if profile, ok := catalog.ProviderByID(name); ok {
		return profile.Label
	}
	return name
}

func withJSON(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Content-Type", "application/json")
		}
		next.ServeHTTP(w, r)
	})
}

func readJSON(body io.Reader, out any) error {
	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return err
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func randomToken(size int) (string, error) {
	raw := make([]byte, size)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
