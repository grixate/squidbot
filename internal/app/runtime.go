package app

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/grixate/squidbot/internal/agent"
	"github.com/grixate/squidbot/internal/channels/telegram"
	"github.com/grixate/squidbot/internal/config"
	"github.com/grixate/squidbot/internal/cron"
	"github.com/grixate/squidbot/internal/heartbeat"
	"github.com/grixate/squidbot/internal/mission"
	"github.com/grixate/squidbot/internal/provider"
	storepkg "github.com/grixate/squidbot/internal/storage/bbolt"
	"github.com/grixate/squidbot/internal/telemetry"
)

type Runtime struct {
	Config         config.Config
	Store          *storepkg.Store
	Engine         *agent.Engine
	Cron           *cron.Service
	Heartbeat      *heartbeat.Service
	Telegram       *telegram.Channel
	Metrics        *telemetry.Metrics
	log            *log.Logger
	cancel         context.CancelFunc
	done           chan struct{}
	mu             sync.RWMutex
	runCtx         context.Context
	telegramCancel context.CancelFunc
}

func BuildRuntime(cfg config.Config, logger *log.Logger) (*Runtime, error) {
	if logger == nil {
		logger = log.Default()
	}
	metrics := &telemetry.Metrics{}
	store, err := storepkg.Open(cfg.Storage.DBPath)
	if err != nil {
		return nil, err
	}
	providerClient, model, err := provider.FromConfig(cfg)
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	engine, err := agent.NewEngine(cfg, providerClient, model, store, metrics, logger)
	if err != nil {
		_ = store.Close()
		return nil, err
	}

	runtime := &Runtime{Config: cfg, Store: store, Engine: engine, Metrics: metrics, log: logger, done: make(chan struct{})}
	runtime.Cron = cron.NewService(store, func(ctx context.Context, job cron.Job) (string, error) {
		response, err := engine.Ask(ctx, agent.InboundMessage{
			SessionID: "cron:" + job.ID,
			RequestID: "",
			Channel:   "cron",
			ChatID:    job.ID,
			SenderID:  "cron",
			Content:   job.Payload.Message,
			CreatedAt: time.Now().UTC(),
		})
		if err != nil {
			return "", err
		}
		if job.Payload.Deliver && job.Payload.Channel != "" && job.Payload.To != "" {
			engine.EmitOutbound(job.Payload.Channel, job.Payload.To, response, map[string]interface{}{"source": "cron", "job_id": job.ID})
		}
		return response, nil
	}, metrics)

	runtime.Heartbeat = heartbeat.NewService(config.WorkspacePath(cfg), time.Duration(cfg.Runtime.HeartbeatIntervalSec)*time.Second, func(ctx context.Context, prompt string) (string, error) {
		response, err := engine.Ask(ctx, agent.InboundMessage{
			SessionID: "system:heartbeat",
			RequestID: "",
			Channel:   "system",
			ChatID:    "heartbeat",
			SenderID:  "heartbeat",
			Content:   prompt,
			CreatedAt: time.Now().UTC(),
		})
		if err == nil {
			engine.RecordHeartbeat(ctx, prompt, response)
		}
		return response, err
	}, metrics)
	runtime.Heartbeat.SetRunObserver(func(record heartbeat.RunRecord) {
		preview := strings.TrimSpace(record.Response)
		if len(preview) > 280 {
			preview = preview[:277] + "..."
		}
		run := mission.HeartbeatRun{
			ID:          "hb-" + strings.ReplaceAll(record.StartedAt.UTC().Format(time.RFC3339Nano), ":", "-"),
			TriggeredBy: record.TriggeredBy,
			Status:      record.Status,
			Error:       record.Error,
			Preview:     preview,
			StartedAt:   record.StartedAt,
			FinishedAt:  record.FinishedAt,
			DurationMS:  record.FinishedAt.Sub(record.StartedAt).Milliseconds(),
		}
		if err := runtime.Store.RecordHeartbeatRun(context.Background(), run); err != nil {
			runtime.log.Printf("failed to record heartbeat run: %v", err)
		}
	})

	if cfg.Channels.Telegram.Enabled {
		runtime.Telegram = telegram.New(cfg.Channels.Telegram, runtime.telegramIngress(), logger)
	}

	return runtime, nil
}

func (r *Runtime) telegramIngress() telegram.IngressHandler {
	return func(ctx context.Context, msg agent.InboundMessage) error {
		_, err := r.Engine.Submit(ctx, msg)
		return err
	}
}

func (r *Runtime) StartGateway(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	r.cancel = cancel

	r.mu.Lock()
	r.runCtx = ctx
	r.mu.Unlock()

	r.Cron.Start()
	r.Heartbeat.Start()

	r.mu.Lock()
	r.startTelegramLocked(ctx)
	r.mu.Unlock()

	go func() {
		defer close(r.done)
		for {
			select {
			case <-ctx.Done():
				return
			case msg := <-r.Engine.Outbound():
				if msg.Channel == "telegram" {
					r.mu.RLock()
					ch := r.Telegram
					r.mu.RUnlock()
					if ch != nil {
						if err := ch.Send(ctx, msg); err != nil {
							r.log.Printf("telegram send failed: %v", err)
						}
					}
				}
			}
		}
	}()

	<-ctx.Done()
	<-r.done
	return nil
}

func (r *Runtime) startTelegramLocked(parentCtx context.Context) {
	if r.Telegram == nil {
		return
	}
	if r.telegramCancel != nil {
		r.telegramCancel()
		r.telegramCancel = nil
	}
	telegramCtx, cancel := context.WithCancel(parentCtx)
	r.telegramCancel = cancel
	ch := r.Telegram
	go func() {
		if err := ch.Start(telegramCtx); err != nil && telegramCtx.Err() == nil {
			r.log.Printf("telegram channel stopped: %v", err)
			if r.cancel != nil {
				r.cancel()
			}
		}
	}()
}

func (r *Runtime) ApplyProvider(ctx context.Context, providerName string, providerCfg config.ProviderConfig) error {
	if err := r.Engine.ApplyProviderConfig(providerName, providerCfg); err != nil {
		return err
	}
	normalized, ok := config.NormalizeProviderName(providerName)
	if !ok {
		return fmt.Errorf("unsupported provider %q", providerName)
	}
	r.mu.Lock()
	r.Config.Providers.Active = normalized
	_ = r.Config.SetProviderByName(normalized, providerCfg)
	r.mu.Unlock()
	return nil
}

func (r *Runtime) ApplyTelegram(ctx context.Context, telegramCfg config.TelegramConfig) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.Config.Channels.Telegram = telegramCfg
	if r.telegramCancel != nil {
		r.telegramCancel()
		r.telegramCancel = nil
	}
	r.Telegram = nil
	if telegramCfg.Enabled {
		r.Telegram = telegram.New(telegramCfg, r.telegramIngress(), r.log)
	}
	if r.runCtx == nil || r.runCtx.Err() != nil {
		return fmt.Errorf("runtime is offline")
	}
	r.startTelegramLocked(r.runCtx)
	return nil
}

func (r *Runtime) Shutdown() error {
	if r.cancel != nil {
		r.cancel()
	}
	r.mu.Lock()
	if r.telegramCancel != nil {
		r.telegramCancel()
		r.telegramCancel = nil
	}
	r.mu.Unlock()
	r.Cron.Stop()
	r.Heartbeat.Stop()
	if err := r.Engine.Close(); err != nil {
		r.log.Printf("engine close error: %v", err)
	}
	return r.Store.Close()
}
