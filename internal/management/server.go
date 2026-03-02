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
	"net"
	"net/http"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/grixate/squidbot/internal/config"
	"github.com/grixate/squidbot/internal/provider"
	"github.com/grixate/squidbot/internal/setupauth"
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
	Host              string
	Port              int
	PublicBaseURL     string
	Logger            *log.Logger
}

type ManagementURLs struct {
	LocalURL      string
	BindURL       string
	RemoteURL     string
	RemoteURLHint string
}

type Server struct {
	mu sync.RWMutex

	cfg        config.Config
	configPath string
	logger     *log.Logger
	host       string
	port       int
	publicURL  string

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
	setupTTL := opts.SetupTokenTTL
	if setupTTL <= 0 {
		setupTTL = defaultSetupTokenTTL
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

	host := strings.TrimSpace(opts.Host)
	if host == "" {
		host = strings.TrimSpace(cfg.Management.Host)
	}
	port := opts.Port
	if port <= 0 {
		port = cfg.Management.Port
	}
	publicURL := strings.TrimRight(strings.TrimSpace(opts.PublicBaseURL), "/")
	if publicURL == "" {
		publicURL = strings.TrimRight(strings.TrimSpace(cfg.Management.PublicBaseURL), "/")
	}

	s := &Server{
		cfg:               cfg,
		configPath:        configPath,
		logger:            logger,
		host:              host,
		port:              port,
		publicURL:         publicURL,
		requireSetupToken: opts.RequireSetupToken,
		setupTokenTTL:     setupTTL,
		passwordMinLength: minPassword,
		sessionIdleTTL:    idleTTL,
		sessionMaxTTL:     maxTTL,
		setupDone:         make(chan struct{}),
		sessions:          map[string]sessionRecord{},
	}

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
	host := strings.TrimSpace(s.host)
	port := s.port
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, strconv.Itoa(port))
}

func (s *Server) BindURL() string {
	host := strings.TrimSpace(s.host)
	if host == "" {
		host = "0.0.0.0"
	}
	return "http://" + net.JoinHostPort(host, strconv.Itoa(s.port))
}

func (s *Server) DisplayURLs() ManagementURLs {
	urls := ManagementURLs{
		LocalURL: s.LocalBaseURL(),
		BindURL:  s.BindURL(),
	}
	if s.publicURL != "" {
		urls.RemoteURL = s.publicURL
	} else if isRemoteHost(s.host) {
		urls.RemoteURLHint = fmt.Sprintf("use http(s)://<public-host-or-ip>:%d", s.port)
	}
	return urls
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
	addr := net.JoinHostPort(strings.TrimSpace(s.host), strconv.Itoa(s.port))

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
	mux.HandleFunc("/api/setup/password/suggest", s.handleSetupPasswordSuggest)
	mux.HandleFunc("/api/setup/provider/test", s.handleSetupProviderTest)
	mux.HandleFunc("/api/setup/complete", s.handleSetupComplete)
	mux.HandleFunc("/api/auth/login", s.handleAuthLogin)
	mux.HandleFunc("/api/auth/logout", s.handleAuthLogout)
	mux.HandleFunc("/api/auth/session", s.handleAuthSession)
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

	type providerTemplate struct {
		ID             string `json:"id"`
		Label          string `json:"label"`
		RequiresAPIKey bool   `json:"requiresApiKey"`
		RequiresModel  bool   `json:"requiresModel"`
		DefaultAPIBase string `json:"defaultApiBase,omitempty"`
		DefaultModel   string `json:"defaultModel,omitempty"`
	}
	providerCatalog := make([]providerTemplate, 0, len(config.SupportedProviders()))
	for _, providerName := range config.SupportedProviders() {
		requiresAPIKey, requiresModel, _ := config.ProviderRequirements(providerName)
		providerCatalog = append(providerCatalog, providerTemplate{
			ID:             providerName,
			Label:          providerLabel(providerName),
			RequiresAPIKey: requiresAPIKey,
			RequiresModel:  requiresModel,
			DefaultAPIBase: config.ProviderDefaultAPIBase(providerName),
			DefaultModel:   config.ProviderDefaultModel(providerName),
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"setupComplete":       setupComplete,
		"requiresSetupToken":  tokenRequired,
		"setupTokenExpiresAt": tokenUntil.Format(time.RFC3339),
		"passwordMinLength":   s.passwordMinLength,
		"providerCatalog":     providerCatalog,
		"current": map[string]any{
			"providers":        currentProviders(cfg),
			"activeProviderId": strings.TrimSpace(cfg.Providers.Active),
			"telegram": map[string]any{
				"enabled":   cfg.Channels.Telegram.Enabled,
				"tokenSet":  strings.TrimSpace(cfg.Channels.Telegram.Token) != "",
				"allowFrom": cfg.Channels.Telegram.AllowFrom,
			},
		},
	})
}

func (s *Server) handleSetupPasswordSuggest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		SetupToken string `json:"setupToken"`
	}
	if err := readJSON(r.Body, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.requireValidSetupToken(req.SetupToken); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	password, err := setupauth.GeneratePassword()
	if err != nil {
		http.Error(w, "failed to generate password", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"password": password})
}

func (s *Server) handleSetupProviderTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		SetupToken string `json:"setupToken"`
		Provider   struct {
			ID      string `json:"id"`
			APIKey  string `json:"apiKey"`
			APIBase string `json:"apiBase"`
			Model   string `json:"model"`
		} `json:"provider"`
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
		APIKey:  strings.TrimSpace(req.Provider.APIKey),
		APIBase: strings.TrimSpace(req.Provider.APIBase),
		Model:   strings.TrimSpace(req.Provider.Model),
	}
	if err := config.ValidateProviderDraft(req.Provider.ID, providerCfg); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	if err := s.liveProviderCheck(ctx, req.Provider.ID, providerCfg); err != nil {
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
		Providers  []struct {
			ID      string `json:"id"`
			APIKey  string `json:"apiKey"`
			APIBase string `json:"apiBase"`
			Model   string `json:"model"`
		} `json:"providers"`
		ActiveProviderID string `json:"activeProviderId"`
		Telegram         *struct {
			Enabled   bool     `json:"enabled"`
			Token     string   `json:"token"`
			AllowFrom []string `json:"allowFrom"`
		} `json:"telegram"`
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
	passwordHash, err := setupauth.HashPassword(password)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	providers := make([]config.WebOnboardingProviderInput, 0, len(req.Providers))
	for _, draft := range req.Providers {
		providers = append(providers, config.WebOnboardingProviderInput{
			ID: strings.TrimSpace(draft.ID),
			ProviderConfig: config.ProviderConfig{
				APIKey:  strings.TrimSpace(draft.APIKey),
				APIBase: strings.TrimSpace(draft.APIBase),
				Model:   strings.TrimSpace(draft.Model),
			},
		})
	}
	var telegramCfg *config.TelegramConfig
	if req.Telegram != nil {
		telegramCfg = &config.TelegramConfig{
			Enabled:   req.Telegram.Enabled,
			Token:     strings.TrimSpace(req.Telegram.Token),
			AllowFrom: req.Telegram.AllowFrom,
		}
	}

	s.mu.RLock()
	cfg := s.cfg
	s.mu.RUnlock()

	nextCfg, err := config.ApplyWebOnboardingInput(cfg, config.WebOnboardingInput{
		Providers:         providers,
		ActiveProviderID:  strings.TrimSpace(req.ActiveProviderID),
		Telegram:          telegramCfg,
		PasswordHash:      passwordHash,
		PasswordUpdatedAt: time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	nextCfg.Management.Host = strings.TrimSpace(s.host)
	nextCfg.Management.Port = s.port
	nextCfg.Management.PublicBaseURL = strings.TrimSpace(s.publicURL)
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
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	s.setupDoneOnce.Do(func() { close(s.setupDone) })
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
	if !setupauth.VerifyPassword(strings.TrimSpace(req.Password), cfg.Auth.PasswordHash) {
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
	if _, ok := s.sessionFromRequest(r); ok {
		authenticated = true
	}

	s.mu.RLock()
	setupComplete := config.IsSetupComplete(s.cfg)
	activeProviderID := strings.TrimSpace(s.cfg.Providers.Active)
	s.mu.RUnlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated":    authenticated,
		"setupComplete":    setupComplete,
		"activeProviderId": activeProviderID,
	})
}

func (s *Server) handleUI(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		http.NotFound(w, r)
		return
	}
	clean := path.Clean(r.URL.Path)

	if strings.HasPrefix(clean, "/assets/") {
		assetPath := "ui/dist" + clean
		if !assetExists(assetPath) {
			http.NotFound(w, r)
			return
		}
		serveAsset(w, assetPath)
		return
	}

	s.mu.RLock()
	setupComplete := config.IsSetupComplete(s.cfg)
	s.mu.RUnlock()
	_, authenticated := s.sessionFromRequest(r)

	if strings.HasPrefix(clean, "/app") {
		if !setupComplete || !authenticated {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		serveAsset(w, "ui/dist/index.html")
		return
	}

	if clean == "." || clean == "/" {
		if setupComplete && authenticated {
			http.Redirect(w, r, "/app", http.StatusSeeOther)
			return
		}
		serveAsset(w, "ui/dist/index.html")
		return
	}

	assetPath := "ui/dist" + clean
	if assetExists(assetPath) {
		serveAsset(w, assetPath)
		return
	}
	serveAsset(w, "ui/dist/index.html")
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
	return err
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
	if now.Sub(rec.LastSeen) > s.sessionIdleTTL || now.Sub(rec.CreatedAt) > s.sessionMaxTTL {
		delete(s.sessions, id)
		return sessionRecord{}, false
	}
	rec.LastSeen = now
	s.sessions[id] = rec
	return rec, true
}

func currentProviders(cfg config.Config) []map[string]any {
	items := map[string]config.ProviderConfig{}
	for id, providerCfg := range cfg.Providers.Registry {
		if providerHasValues(providerCfg) || strings.TrimSpace(cfg.Providers.Active) == id {
			items[id] = providerCfg
		}
	}
	for _, providerID := range config.SupportedProviders() {
		if _, exists := items[providerID]; exists {
			continue
		}
		providerCfg, ok := cfg.ProviderByName(providerID)
		if !ok {
			continue
		}
		if providerHasValues(providerCfg) || strings.TrimSpace(cfg.Providers.Active) == providerID {
			items[providerID] = providerCfg
		}
	}

	ids := make([]string, 0, len(items))
	for id := range items {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	out := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		item := items[id]
		out = append(out, map[string]any{
			"id":        id,
			"label":     providerLabel(id),
			"apiBase":   strings.TrimSpace(item.APIBase),
			"model":     strings.TrimSpace(item.Model),
			"hasApiKey": strings.TrimSpace(item.APIKey) != "",
		})
	}
	return out
}

func providerHasValues(providerCfg config.ProviderConfig) bool {
	return strings.TrimSpace(providerCfg.APIKey) != "" ||
		strings.TrimSpace(providerCfg.APIBase) != "" ||
		strings.TrimSpace(providerCfg.Model) != ""
}

func isRemoteHost(host string) bool {
	host = strings.TrimSpace(host)
	switch host {
	case "", "127.0.0.1", "localhost", "::1":
		return false
	default:
		return true
	}
}

func providerLabel(name string) string {
	switch name {
	case config.ProviderOpenRouter:
		return "OpenRouter"
	case config.ProviderAnthropic:
		return "Anthropic"
	case config.ProviderOpenAI:
		return "OpenAI"
	case config.ProviderGemini:
		return "Gemini"
	case config.ProviderOllama:
		return "Ollama"
	case config.ProviderLMStudio:
		return "LM Studio"
	default:
		return name
	}
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
	return dec.Decode(out)
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
