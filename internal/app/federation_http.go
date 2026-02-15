package app

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/grixate/squidbot/internal/federation"
)

func (r *Runtime) startFederationHTTP(ctx context.Context) {
	if r == nil || r.Engine == nil {
		return
	}
	cfg := r.Config
	if !cfg.Runtime.Federation.Enabled {
		return
	}
	listenAddr := strings.TrimSpace(cfg.Runtime.Federation.ListenAddr)
	if listenAddr == "" {
		return
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/federation/health", r.handleFederationHealth)
	mux.HandleFunc("/api/federation/delegations", r.handleFederationDelegations)
	mux.HandleFunc("/api/federation/delegations/", r.handleFederationDelegationByID)
	r.federationSrv = &http.Server{Addr: listenAddr, Handler: mux}
	go func() {
		<-ctx.Done()
		_ = r.federationSrv.Shutdown(context.Background())
	}()
	go func() {
		if err := r.federationSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			r.log.Printf("federation http stopped: %v", err)
		}
	}()
	go func() {
		r.refreshFederationPeerHealth(ctx)
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				r.refreshFederationPeerHealth(ctx)
			}
		}
	}()
}

func (r *Runtime) federationAuth(req *http.Request) (string, int, string) {
	cfg := r.Config
	if !cfg.Runtime.Federation.Enabled {
		return "", http.StatusNotFound, "federation disabled"
	}
	originNodeID := strings.TrimSpace(req.Header.Get("X-Squidbot-Node-ID"))
	if originNodeID == "" {
		return "", http.StatusUnauthorized, "missing X-Squidbot-Node-ID"
	}
	if len(cfg.Runtime.Federation.AllowFromNodeIDs) > 0 {
		allowed := false
		for _, item := range cfg.Runtime.Federation.AllowFromNodeIDs {
			if strings.EqualFold(strings.TrimSpace(item), originNodeID) {
				allowed = true
				break
			}
		}
		if !allowed {
			return "", http.StatusForbidden, "node is not allowlisted"
		}
	}
	authz := strings.TrimSpace(req.Header.Get("Authorization"))
	if authz == "" || !strings.HasPrefix(authz, "Bearer ") {
		return "", http.StatusUnauthorized, "missing bearer token"
	}
	token := strings.TrimSpace(strings.TrimPrefix(authz, "Bearer "))
	if token == "" {
		return "", http.StatusUnauthorized, "missing bearer token"
	}
	for _, peer := range cfg.Runtime.Federation.Peers {
		if !peer.Enabled {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(peer.ID), originNodeID) {
			continue
		}
		if strings.TrimSpace(peer.AuthToken) == token {
			return originNodeID, 0, ""
		}
		return "", http.StatusUnauthorized, "invalid token"
	}
	return "", http.StatusUnauthorized, "unknown peer node"
}

func (r *Runtime) handleFederationHealth(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	_, status, msg := r.federationAuth(req)
	if status != 0 {
		http.Error(w, msg, status)
		return
	}
	health, err := r.Engine.FederationHealth(req.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeFederationJSON(w, http.StatusOK, health)
}

func (r *Runtime) handleFederationDelegations(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	originNodeID, status, msg := r.federationAuth(req)
	if status != 0 {
		http.Error(w, msg, status)
		return
	}
	idempotencyKey := strings.TrimSpace(req.Header.Get("X-Idempotency-Key"))
	if idempotencyKey == "" {
		http.Error(w, "missing X-Idempotency-Key", http.StatusBadRequest)
		return
	}
	var payload federation.DelegationRequest
	if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	run, err := r.Engine.FederationSubmit(req.Context(), payload, originNodeID, idempotencyKey)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeFederationJSON(w, http.StatusOK, run)
}

func (r *Runtime) handleFederationDelegationByID(w http.ResponseWriter, req *http.Request) {
	originNodeID, status, msg := r.federationAuth(req)
	if status != 0 {
		http.Error(w, msg, status)
		return
	}
	_ = originNodeID
	rest := strings.Trim(strings.TrimPrefix(req.URL.Path, "/api/federation/delegations/"), "/")
	if rest == "" {
		http.NotFound(w, req)
		return
	}
	parts := strings.Split(rest, "/")
	runID := parts[0]
	if runID == "" {
		http.NotFound(w, req)
		return
	}
	if len(parts) == 1 {
		if req.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		run, err := r.Engine.FederationStatus(req.Context(), runID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		writeFederationJSON(w, http.StatusOK, run)
		return
	}
	action := parts[1]
	switch action {
	case "result":
		if req.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		run, err := r.Engine.FederationResult(req.Context(), runID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeFederationJSON(w, http.StatusOK, run)
	case "cancel":
		if req.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		run, err := r.Engine.FederationCancel(req.Context(), runID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeFederationJSON(w, http.StatusOK, run)
	default:
		http.NotFound(w, req)
	}
}

func writeFederationJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func (r *Runtime) refreshFederationPeerHealth(ctx context.Context) {
	if r == nil || r.Engine == nil || !r.Config.Runtime.Federation.Enabled {
		return
	}
	cfg := r.Config
	client := federation.NewClient(time.Duration(max(cfg.Runtime.Federation.RequestTimeoutSec, 1)) * time.Second)
	nodeID := strings.TrimSpace(cfg.Runtime.Federation.NodeID)
	if nodeID == "" {
		host, err := os.Hostname()
		if err == nil && strings.TrimSpace(host) != "" {
			nodeID = "squidbot-" + strings.ToLower(strings.TrimSpace(host))
		} else {
			nodeID = "squidbot-node"
		}
	}
	for _, peer := range cfg.Runtime.Federation.Peers {
		if !peer.Enabled {
			continue
		}
		health := federation.PeerHealth{
			PeerID:    strings.TrimSpace(peer.ID),
			Available: false,
			UpdatedAt: time.Now().UTC(),
		}
		started := time.Now().UTC()
		resp, err := client.Health(ctx, peer, nodeID)
		health.ResponseTime = time.Since(started).Milliseconds()
		if err == nil {
			health.Available = resp.Available
			health.QueueDepth = resp.QueueDepth
			health.MaxQueue = resp.MaxQueue
			health.ActiveRuns = resp.ActiveRuns
			health.Error = resp.Error
		} else {
			health.Error = err.Error()
		}
		if putErr := r.Store.PutFederationPeerHealth(context.Background(), health); putErr != nil {
			r.log.Printf("failed to store federation peer health peer=%s err=%v", peer.ID, putErr)
		}
	}
}

func max(v, floor int) int {
	if v < floor {
		return floor
	}
	return v
}
