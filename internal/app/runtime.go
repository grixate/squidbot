package app

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/grixate/squidbot/internal/agent"
	channelreg "github.com/grixate/squidbot/internal/channels"
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
	Config    config.Config
	Store     *storepkg.Store
	Engine    *agent.Engine
	Cron      *cron.Service
	Heartbeat *heartbeat.Service
	Channels  *channelreg.Registry
	Metrics   *telemetry.Metrics
	log       *log.Logger
	cancel    context.CancelFunc
	done      chan struct{}
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

	runtime.Channels = channelreg.NewRegistry(logger)
	runtime.registerChannels(cfg)

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

	r.Cron.Start()
	r.Heartbeat.Start()

	go func() {
		defer close(r.done)
		for {
			select {
			case <-ctx.Done():
				return
			case msg := <-r.Engine.Outbound():
				if r.Channels != nil {
					if err := r.Channels.Send(ctx, msg); err != nil {
						r.log.Printf("channel send failed channel=%s: %v", msg.Channel, err)
					}
				}
			}
		}
	}()

	if r.Channels != nil {
		r.Channels.StartAll(ctx)
	}

	<-ctx.Done()
	<-r.done
	return nil
}

func (r *Runtime) Shutdown() error {
	if r.cancel != nil {
		r.cancel()
	}
	r.Cron.Stop()
	r.Heartbeat.Stop()
	if err := r.Engine.Close(); err != nil {
		r.log.Printf("engine close error: %v", err)
	}
	return r.Store.Close()
}

func (r *Runtime) registerChannels(cfg config.Config) {
	if r.Channels == nil {
		return
	}
	if cfg.Channels.Registry == nil {
		cfg.Channels.Registry = map[string]config.GenericChannelConfig{}
	}
	hasTelegram := false
	for channelID, channelCfg := range cfg.Channels.Registry {
		if !channelCfg.Enabled {
			continue
		}
		switch channelID {
		case "telegram":
			hasTelegram = true
			telegramCfg := config.TelegramConfig{
				Enabled:   channelCfg.Enabled,
				Token:     strings.TrimSpace(channelCfg.Token),
				AllowFrom: channelCfg.AllowFrom,
			}
			if strings.TrimSpace(telegramCfg.Token) == "" {
				telegramCfg = cfg.Channels.Telegram
			}
			adapter := &telegramAdapter{id: "telegram", channel: telegram.New(telegramCfg, r.telegramIngress(), r.log)}
			if err := r.Channels.Register(adapter); err != nil {
				r.log.Printf("register telegram channel failed: %v", err)
			}
		case "discord", "slack", "irc", "webchat":
			if strings.TrimSpace(channelCfg.Endpoint) != "" {
				_ = r.Channels.Register(channelreg.NewWebhookAdapter(channelID, channelCfg, r.log))
			} else {
				_ = r.Channels.Register(channelreg.NewNoopAdapter(channelID, r.log))
			}
		default:
			_ = r.Channels.Register(channelreg.NewWebhookAdapter(channelID, channelCfg, r.log))
		}
	}
	if !hasTelegram && cfg.Channels.Telegram.Enabled {
		adapter := &telegramAdapter{id: "telegram", channel: telegram.New(cfg.Channels.Telegram, r.telegramIngress(), r.log)}
		if err := r.Channels.Register(adapter); err != nil {
			r.log.Printf("register telegram channel failed: %v", err)
		}
	}
}

type telegramAdapter struct {
	id      string
	channel *telegram.Channel
}

func (a *telegramAdapter) ID() string { return a.id }

func (a *telegramAdapter) Start(ctx context.Context) error {
	if a.channel == nil {
		return nil
	}
	return a.channel.Start(ctx)
}

func (a *telegramAdapter) Send(ctx context.Context, msg agent.OutboundMessage) error {
	if a.channel == nil {
		return nil
	}
	return a.channel.Send(ctx, msg)
}
