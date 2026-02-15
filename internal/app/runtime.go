package app

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
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
	Config     config.Config
	Store      *storepkg.Store
	Engine     *agent.Engine
	Cron       *cron.Service
	Heartbeat  *heartbeat.Service
	Channels   *channelreg.Registry
	Metrics    *telemetry.Metrics
	log        *log.Logger
	cancel     context.CancelFunc
	done       chan struct{}
	metricsSrv *http.Server
	federationSrv *http.Server
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
	if err := runtime.registerChannels(cfg); err != nil {
		_ = engine.Close()
		_ = store.Close()
		return nil, err
	}

	return runtime, nil
}

func (r *Runtime) telegramIngress() telegram.IngressHandler {
	return func(ctx context.Context, msg agent.InboundMessage) error {
		if strings.TrimSpace(msg.Channel) == "" {
			msg.Channel = "telegram"
		}
		_, err := r.Engine.Submit(ctx, msg)
		return err
	}
}

func (r *Runtime) channelIngress(channelID string) channelreg.IngressHandler {
	channelID = strings.ToLower(strings.TrimSpace(channelID))
	return func(ctx context.Context, msg agent.InboundMessage) error {
		if strings.TrimSpace(msg.Channel) == "" {
			msg.Channel = channelID
		}
		if strings.TrimSpace(msg.SessionID) == "" {
			msg.SessionID = msg.Channel + ":" + msg.ChatID
		}
		_, err := r.Engine.Submit(ctx, msg)
		return err
	}
}

func (r *Runtime) channelAsk(channelID string) channelreg.AskHandler {
	channelID = strings.ToLower(strings.TrimSpace(channelID))
	return func(ctx context.Context, msg agent.InboundMessage) (string, error) {
		if strings.TrimSpace(msg.Channel) == "" {
			msg.Channel = channelID
		}
		if strings.TrimSpace(msg.SessionID) == "" {
			msg.SessionID = msg.Channel + ":" + msg.ChatID
		}
		return r.Engine.Ask(ctx, msg)
	}
}

func (r *Runtime) channelAskStream(channelID string) channelreg.AskStreamHandler {
	channelID = strings.ToLower(strings.TrimSpace(channelID))
	return func(ctx context.Context, msg agent.InboundMessage, sink agent.StreamSink) error {
		if strings.TrimSpace(msg.Channel) == "" {
			msg.Channel = channelID
		}
		if strings.TrimSpace(msg.SessionID) == "" {
			msg.SessionID = msg.Channel + ":" + msg.ChatID
		}
		return r.Engine.AskStream(ctx, msg, sink)
	}
}

func (r *Runtime) StartGateway(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	r.cancel = cancel

	r.Cron.Start()
	r.Heartbeat.Start()
	r.startMetricsHTTP()
	r.startFederationHTTP(ctx)

	go func() {
		defer close(r.done)
		for {
			select {
			case <-ctx.Done():
				return
			case msg := <-r.Engine.Outbound():
				if r.Channels != nil {
					if err := r.Channels.Send(ctx, msg); err != nil {
						traceID, _ := msg.Metadata["trace_id"].(string)
						r.log.Printf("event=channel_send_failed trace_id=%s channel=%s chat_id=%s err=%v", traceID, msg.Channel, msg.ChatID, err)
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
	if r.metricsSrv != nil {
		_ = r.metricsSrv.Shutdown(context.Background())
	}
	if r.federationSrv != nil {
		_ = r.federationSrv.Shutdown(context.Background())
	}
	r.Cron.Stop()
	r.Heartbeat.Stop()
	if err := r.Engine.Close(); err != nil {
		r.log.Printf("engine close error: %v", err)
	}
	return r.Store.Close()
}

func (r *Runtime) startMetricsHTTP() {
	if r == nil || r.Metrics == nil {
		return
	}
	if !(r.Config.Features.MetricsHTTP || r.Config.Runtime.MetricsHTTP.Enabled) {
		return
	}
	listenAddr := strings.TrimSpace(r.Config.Runtime.MetricsHTTP.ListenAddr)
	if listenAddr == "" {
		return
	}
	authToken := strings.TrimSpace(r.Config.Runtime.MetricsHTTP.AuthToken)
	localhostOnly := r.Config.Runtime.MetricsHTTP.LocalhostOnly
	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, req *http.Request) {
		if localhostOnly {
			host, _, err := net.SplitHostPort(req.RemoteAddr)
			if err == nil {
				ip := net.ParseIP(host)
				if ip == nil || !ip.IsLoopback() {
					http.Error(w, "forbidden", http.StatusForbidden)
					return
				}
			}
		}
		if authToken != "" {
			token := req.Header.Get("Authorization")
			if token != "Bearer "+authToken {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_, _ = w.Write([]byte(telemetry.PrometheusText(r.Metrics.Snapshot())))
	})
	r.metricsSrv = &http.Server{Addr: listenAddr, Handler: mux}
	go func() {
		if err := r.metricsSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			r.log.Printf("metrics http stopped: %v", err)
		}
	}()
}

func (r *Runtime) registerChannels(cfg config.Config) error {
	if r.Channels == nil {
		return nil
	}
	if cfg.Channels.Registry == nil {
		cfg.Channels.Registry = map[string]config.GenericChannelConfig{}
	}
	hasTelegram := false
	strictWave1 := cfg.Features.ChannelsWave1
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
		case "slack":
			if strictWave1 {
				if err := validateSlackConfig(channelCfg); err != nil {
					return fmt.Errorf("channel %q misconfigured: %w", channelID, err)
				}
			}
			if canUseSlackNative(channelCfg) || strictWave1 {
				if err := r.Channels.Register(channelreg.NewSlackAdapter(channelCfg, r.channelIngress("slack"), r.log)); err != nil {
					return err
				}
				continue
			}
			if strings.TrimSpace(channelCfg.Endpoint) != "" {
				if err := r.Channels.Register(channelreg.NewWebhookAdapter(channelID, channelCfg, r.log)); err != nil {
					return err
				}
			} else {
				if err := r.Channels.Register(channelreg.NewNoopAdapter(channelID, r.log)); err != nil {
					return err
				}
			}
		case "discord":
			if strictWave1 {
				if err := validateDiscordConfig(channelCfg); err != nil {
					return fmt.Errorf("channel %q misconfigured: %w", channelID, err)
				}
			}
			if canUseDiscordNative(channelCfg) || strictWave1 {
				if err := r.Channels.Register(channelreg.NewDiscordAdapter(channelCfg, r.channelIngress("discord"), r.log)); err != nil {
					return err
				}
				continue
			}
			if strings.TrimSpace(channelCfg.Endpoint) != "" {
				if err := r.Channels.Register(channelreg.NewWebhookAdapter(channelID, channelCfg, r.log)); err != nil {
					return err
				}
			} else {
				if err := r.Channels.Register(channelreg.NewNoopAdapter(channelID, r.log)); err != nil {
					return err
				}
			}
		case "webchat":
			if strictWave1 {
				if err := validateWebChatConfig(channelCfg); err != nil {
					return fmt.Errorf("channel %q misconfigured: %w", channelID, err)
				}
			}
			if canUseWebChatNative(channelCfg) || strictWave1 {
				if err := r.Channels.Register(channelreg.NewWebChatAdapter(channelCfg, r.channelIngress("webchat"), r.channelAsk("webchat"), r.channelAskStream("webchat"), r.log)); err != nil {
					return err
				}
				continue
			}
			if strings.TrimSpace(channelCfg.Endpoint) != "" {
				if err := r.Channels.Register(channelreg.NewWebhookAdapter(channelID, channelCfg, r.log)); err != nil {
					return err
				}
			} else {
				if err := r.Channels.Register(channelreg.NewNoopAdapter(channelID, r.log)); err != nil {
					return err
				}
			}
		case "whatsapp":
			if canUseWhatsAppNative(channelCfg) {
				if err := validateWhatsAppConfig(channelCfg); err != nil {
					return fmt.Errorf("channel %q misconfigured: %w", channelID, err)
				}
				if err := r.Channels.Register(channelreg.NewWhatsAppAdapter(channelCfg, r.channelIngress("whatsapp"), r.log)); err != nil {
					return err
				}
				continue
			}
			if strings.TrimSpace(channelCfg.Endpoint) != "" {
				if err := r.Channels.Register(channelreg.NewWebhookAdapter(channelID, channelCfg, r.log)); err != nil {
					return err
				}
			} else {
				if err := r.Channels.Register(channelreg.NewNoopAdapter(channelID, r.log)); err != nil {
					return err
				}
			}
		case "irc":
			if strings.TrimSpace(channelCfg.Endpoint) != "" {
				if err := r.Channels.Register(channelreg.NewWebhookAdapter(channelID, channelCfg, r.log)); err != nil {
					return err
				}
			} else {
				if err := r.Channels.Register(channelreg.NewNoopAdapter(channelID, r.log)); err != nil {
					return err
				}
			}
		default:
			if err := r.Channels.Register(channelreg.NewWebhookAdapter(channelID, channelCfg, r.log)); err != nil {
				return err
			}
		}
	}
	if !hasTelegram && cfg.Channels.Telegram.Enabled {
		adapter := &telegramAdapter{id: "telegram", channel: telegram.New(cfg.Channels.Telegram, r.telegramIngress(), r.log)}
		if err := r.Channels.Register(adapter); err != nil {
			r.log.Printf("register telegram channel failed: %v", err)
		}
	}
	return nil
}

func channelMeta(cfg config.GenericChannelConfig, key string) string {
	if cfg.Metadata == nil {
		return ""
	}
	return strings.TrimSpace(cfg.Metadata[key])
}

func canUseSlackNative(cfg config.GenericChannelConfig) bool {
	return strings.TrimSpace(cfg.Token) != "" || channelMeta(cfg, "listen_addr") != ""
}

func canUseDiscordNative(cfg config.GenericChannelConfig) bool {
	return strings.TrimSpace(cfg.Token) != "" || channelMeta(cfg, "listen_addr") != "" || channelMeta(cfg, "public_key") != ""
}

func canUseWebChatNative(cfg config.GenericChannelConfig) bool {
	return channelMeta(cfg, "listen_addr") != ""
}

func canUseWhatsAppNative(cfg config.GenericChannelConfig) bool {
	return channelMeta(cfg, "listen_addr") != "" || channelMeta(cfg, "phone_number_id") != ""
}

func validateSlackConfig(cfg config.GenericChannelConfig) error {
	if strings.TrimSpace(cfg.Token) == "" {
		return fmt.Errorf("token missing")
	}
	if channelMeta(cfg, "listen_addr") == "" {
		return fmt.Errorf("metadata.listen_addr missing")
	}
	return nil
}

func validateDiscordConfig(cfg config.GenericChannelConfig) error {
	if strings.TrimSpace(cfg.Token) == "" {
		return fmt.Errorf("token missing")
	}
	if channelMeta(cfg, "listen_addr") == "" {
		return fmt.Errorf("metadata.listen_addr missing")
	}
	if channelMeta(cfg, "public_key") == "" {
		return fmt.Errorf("metadata.public_key missing")
	}
	return nil
}

func validateWebChatConfig(cfg config.GenericChannelConfig) error {
	if channelMeta(cfg, "listen_addr") == "" {
		return fmt.Errorf("metadata.listen_addr missing")
	}
	return nil
}

func validateWhatsAppConfig(cfg config.GenericChannelConfig) error {
	if strings.TrimSpace(cfg.Token) == "" {
		return fmt.Errorf("token missing")
	}
	if channelMeta(cfg, "listen_addr") == "" {
		return fmt.Errorf("metadata.listen_addr missing")
	}
	if channelMeta(cfg, "phone_number_id") == "" {
		return fmt.Errorf("metadata.phone_number_id missing")
	}
	return nil
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
