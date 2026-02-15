package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/grixate/squidbot/internal/agent"
	"github.com/grixate/squidbot/internal/app"
	"github.com/grixate/squidbot/internal/budget"
	"github.com/grixate/squidbot/internal/config"
	"github.com/grixate/squidbot/internal/cron"
	"github.com/grixate/squidbot/internal/memory"
	"github.com/grixate/squidbot/internal/plugins"
	"github.com/grixate/squidbot/internal/skills"
	storepkg "github.com/grixate/squidbot/internal/storage/bbolt"
	"github.com/grixate/squidbot/internal/subagent"
)

const squidbotRomanBanner = "                                 o8o        .o8   .o8                     .\n" +
	"                                 `\"'       \"888  \"888                   .o8\n" +
	" .oooo.o  .ooooo oo oooo  oooo  oooo   .oooo888   888oooo.   .ooooo.  .o888oo\n" +
	"d88(  \"8 d88' `888  `888  `888  `888  d88' `888   d88' `88b d88' `88b   888\n" +
	"`\"Y88b.  888   888   888   888   888  888   888   888   888 888   888   888\n" +
	"o.  )88b 888   888   888   888   888  888   888   888   888 888   888   888 .\n" +
	"8\"\"888P' `V8bod888   `V88V\"V8P' o888o `Y8bod88P\"  `Y8bod8P' `Y8bod8P'   \"888\"\n" +
	"               888.\n" +
	"               8P'\n" +
	"               \""

func main() {
	logger := log.New(os.Stderr, "", log.LstdFlags)
	root := newRootCmd(logger)
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRootCmd(logger *log.Logger) *cobra.Command {
	var configPath string
	root := &cobra.Command{
		Use:   "squidbot",
		Short: "squidbot - Go-native personal AI assistant",
		RunE:  func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	root.PersistentFlags().StringVar(&configPath, "config", "", "config file path")

	root.AddCommand(onboardCmd(configPath))
	root.AddCommand(statusCmd(configPath))
	root.AddCommand(agentCmd(configPath, logger))
	root.AddCommand(gatewayCmd(configPath, logger))
	root.AddCommand(telegramCmd(configPath))
	root.AddCommand(cronCmd(configPath, logger))
	root.AddCommand(subagentsCmd(configPath))
	root.AddCommand(skillsCmd(configPath))
	root.AddCommand(budgetCmd(configPath))
	root.AddCommand(doctorCmd(configPath))
	return root
}

func printBanner(w io.Writer) {
	fmt.Fprintln(w, squidbotRomanBanner)
}

func resolvedConfigPath(path string) string {
	if strings.TrimSpace(path) != "" {
		return path
	}
	return config.ConfigPath()
}

func loadCfg(path string) (config.Config, error) {
	cfg, err := config.Load(path)
	if err != nil {
		return cfg, err
	}
	cfg.Agents.Defaults.Workspace = config.WorkspacePath(cfg)
	if cfg.Storage.DBPath == "" {
		cfg.Storage.DBPath = config.DataRoot() + "/squidbot.db"
	}
	if strings.TrimSpace(cfg.Memory.IndexPath) == "" {
		cfg.Memory.IndexPath = filepath.Join(config.DataRoot(), "memory_index.db")
	}
	if len(cfg.Skills.Paths) == 0 {
		cfg.Skills.Paths = []string{filepath.Join(cfg.Agents.Defaults.Workspace, "skills")}
	}
	return cfg, nil
}

func onboardCmd(configPath string) *cobra.Command {
	var providerName string
	var apiKey string
	var apiBase string
	var model string
	var nonInteractive bool
	var verifyGeminiCLI bool
	var telegramEnabled bool
	var telegramToken string
	var telegramAllowFrom []string
	var channelEnabledIDs []string
	var channelEndpoints []string
	var channelAuthTokens []string

	cmd := &cobra.Command{
		Use:   "onboard",
		Short: "Initialize squidbot config and workspace",
		RunE: func(cmd *cobra.Command, args []string) error {
			printBanner(cmd.OutOrStdout())
			cfg, err := loadCfg(configPath)
			if err != nil {
				return err
			}

			result, err := config.RunOnboarding(cmd.Context(), cfg, config.OnboardingOptions{
				Provider:             providerName,
				APIKey:               apiKey,
				APIBase:              apiBase,
				Model:                model,
				NonInteractive:       nonInteractive,
				VerifyGeminiCLI:      verifyGeminiCLI,
				TelegramEnabledSet:   cmd.Flags().Changed("telegram-enabled"),
				TelegramEnabled:      telegramEnabled,
				TelegramTokenSet:     cmd.Flags().Changed("telegram-token"),
				TelegramToken:        telegramToken,
				TelegramAllowFromSet: cmd.Flags().Changed("telegram-allow-from"),
				TelegramAllowFrom:    telegramAllowFrom,
				ChannelEnabledIDs:    channelEnabledIDs,
				ChannelEndpoints:     channelEndpoints,
				ChannelAuthTokens:    channelAuthTokens,
				In:                   cmd.InOrStdin(),
				Out:                  cmd.OutOrStdout(),
			})
			if err != nil {
				return err
			}
			if err := config.Save(configPath, result.Config); err != nil {
				return err
			}
			if err := config.EnsureFilesystem(result.Config); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Saved config at %s\n", resolvedConfigPath(configPath))
			fmt.Fprintf(cmd.OutOrStdout(), "Workspace ready at %s\n", config.WorkspacePath(result.Config))
			fmt.Fprintf(cmd.OutOrStdout(), "Active provider: %s\n", result.Provider)
			if result.GeminiCLIVerifyRan && result.GeminiCLIVerified {
				fmt.Fprintln(cmd.OutOrStdout(), "Gemini CLI verification passed")
			}
			for _, warning := range result.Warnings {
				fmt.Fprintf(cmd.OutOrStdout(), "Warning: %s\n", warning)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Next: run `squidbot agent -m \"hello\"`")
			return nil
		},
	}
	cmd.Flags().StringVar(&providerName, "provider", "", "Provider id")
	cmd.Flags().StringVar(&apiKey, "api-key", "", "Provider API key")
	cmd.Flags().StringVar(&apiBase, "api-base", "", "Provider API base URL")
	cmd.Flags().StringVar(&model, "model", "", "Provider model")
	cmd.Flags().BoolVar(&nonInteractive, "non-interactive", false, "Disable prompts and require explicit inputs")
	cmd.Flags().BoolVar(&verifyGeminiCLI, "verify-gemini-cli", false, "Verify Gemini CLI connectivity during onboarding")
	cmd.Flags().BoolVar(&telegramEnabled, "telegram-enabled", false, "Enable Telegram channel")
	cmd.Flags().StringVar(&telegramToken, "telegram-token", "", "Telegram bot token")
	cmd.Flags().StringSliceVar(&telegramAllowFrom, "telegram-allow-from", nil, "Telegram allow list entry (repeatable or comma-separated)")
	cmd.Flags().StringSliceVar(&channelEnabledIDs, "channel-enable", nil, "Enable channel id (repeatable)")
	cmd.Flags().StringSliceVar(&channelEndpoints, "channel-endpoint", nil, "Channel endpoint in id=url form (repeatable)")
	cmd.Flags().StringSliceVar(&channelAuthTokens, "channel-auth-token", nil, "Channel auth token in id=token form (repeatable)")
	return cmd
}

func statusCmd(configPath string) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show squidbot status",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(configPath)
			if err != nil {
				return err
			}
			st := config.BuildStatus(cfg)
			fmt.Printf("Config: %s [%v]\n", st.ConfigPath, st.ConfigOK)
			fmt.Printf("Workspace: %s [%v]\n", st.Workspace, st.WorkspaceOK)
			fmt.Printf("Data root: %s [%v]\n", st.DataRoot, st.DataRootOK)
			fmt.Printf("Model: %s\n", cfg.Agents.Defaults.Model)
			if providerName, _ := cfg.PrimaryProvider(); providerName != "" {
				fmt.Printf("Detected provider: %s\n", providerName)
			}
			fmt.Printf("Active provider: %s\n", cfg.Providers.Active)
			if err := config.ValidateActiveProvider(cfg); err != nil {
				fmt.Printf("Provider ready: false (%v)\n", err)
			} else {
				fmt.Println("Provider ready: true")
			}
			fmt.Printf("Storage backend: %s\n", cfg.Storage.Backend)
			fmt.Printf("Telegram enabled: %v\n", cfg.Channels.Telegram.Enabled)
			fmt.Printf("Feature flags: streaming=%v channelsWave1=%v semanticMemory=%v plugins=%v metricsHttp=%v\n",
				cfg.Features.Streaming, cfg.Features.ChannelsWave1, cfg.Features.SemanticMemory, cfg.Features.Plugins, cfg.Features.MetricsHTTP)
			fmt.Printf("Tool policy: execEnabled=%v parentWrite=%v subagentWrite=%v\n",
				cfg.Tools.Exec.Enabled, cfg.Tools.Filesystem.ParentWriteEnabled, cfg.Tools.Filesystem.SubagentWriteEnabled)
			fmt.Printf("Plugins runtime: enabled=%v paths=%d timeoutSec=%d maxConcurrent=%d maxProcesses=%d\n",
				cfg.Runtime.Plugins.Enabled, len(cfg.Runtime.Plugins.Paths), cfg.Runtime.Plugins.DefaultTimeoutSec, cfg.Runtime.Plugins.MaxConcurrent, cfg.Runtime.Plugins.MaxProcesses)
			fmt.Printf("Federation runtime: enabled=%v nodeId=%s listen=%s peers=%d allowFrom=%d retries=%d backoffMs=%d autoFallback=%v\n",
				cfg.Runtime.Federation.Enabled,
				strings.TrimSpace(cfg.Runtime.Federation.NodeID),
				cfg.Runtime.Federation.ListenAddr,
				len(cfg.Runtime.Federation.Peers),
				len(cfg.Runtime.Federation.AllowFromNodeIDs),
				cfg.Runtime.Federation.MaxRetries,
				cfg.Runtime.Federation.RetryBackoffMs,
				cfg.Runtime.Federation.AutoFallback,
			)
			fmt.Printf("Metrics HTTP: enabled=%v listen=%s localhostOnly=%v authTokenSet=%v\n",
				cfg.Runtime.MetricsHTTP.Enabled, cfg.Runtime.MetricsHTTP.ListenAddr, cfg.Runtime.MetricsHTTP.LocalhostOnly, strings.TrimSpace(cfg.Runtime.MetricsHTTP.AuthToken) != "")
			fmt.Printf("Semantic memory: enabled=%v topKCandidates=%d rerankTopK=%d\n",
				cfg.Memory.Semantic.Enabled, cfg.Memory.Semantic.TopKCandidates, cfg.Memory.Semantic.RerankTopK)
			fmt.Printf("Skills runtime: enabled=%v paths=%d maxActive=%d allowZip=%v refreshSec=%d\n",
				cfg.Skills.Enabled, len(cfg.Skills.Paths), cfg.Skills.MaxActive, cfg.Skills.AllowZip, cfg.Skills.RefreshIntervalSec)
			return nil
		},
	}
}

func agentCmd(configPath string, logger *log.Logger) *cobra.Command {
	var message string
	var sessionID string
	var stream bool
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Chat with squidbot directly",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(configPath)
			if err != nil {
				return err
			}
			if err := config.ValidateActiveProvider(cfg); err != nil {
				return fmt.Errorf("provider setup incomplete: %w. Run `squidbot onboard`", err)
			}
			if err := config.EnsureFilesystem(cfg); err != nil {
				return err
			}
			runtime, err := app.BuildRuntime(cfg, logger)
			if err != nil {
				return err
			}
			defer runtime.Shutdown()

			if strings.TrimSpace(sessionID) == "" {
				sessionID = "cli:default"
			}

			if strings.TrimSpace(message) != "" {
				inbound := agent.InboundMessage{
					SessionID: sessionID,
					RequestID: "",
					Channel:   "cli",
					ChatID:    "direct",
					SenderID:  "user",
					Content:   message,
					CreatedAt: time.Now().UTC(),
				}
				if stream {
					var final string
					err := runtime.Engine.AskStream(context.Background(), inbound, agent.StreamSinkFunc(func(ctx context.Context, event agent.StreamEvent) error {
						switch event.Type {
						case "assistant_delta":
							fmt.Print(event.Delta)
						case "final":
							final = event.Content
						case "error":
							return errors.New(event.Error)
						}
						return nil
					}))
					if err != nil {
						return err
					}
					if strings.TrimSpace(final) != "" {
						fmt.Println()
					}
					return nil
				}
				resp, err := runtime.Engine.Ask(context.Background(), inbound)
				if err != nil {
					return err
				}
				fmt.Println(resp)
				return nil
			}

			fmt.Println("Interactive mode (Ctrl+C to exit)")
			reader := bufio.NewReader(os.Stdin)
			stopAsync := make(chan struct{})
			go func() {
				for {
					select {
					case <-stopAsync:
						return
					case msg := <-runtime.Engine.Outbound():
						if msg.Channel != "cli" {
							continue
						}
						source, _ := msg.Metadata["source"].(string)
						if source != "subagent" {
							continue
						}
						msgSession, _ := msg.Metadata["session_id"].(string)
						if strings.TrimSpace(msgSession) != "" && msgSession != sessionID {
							continue
						}
						fmt.Printf("\n\n[async]\n%s\n\nYou: ", msg.Content)
					}
				}
			}()
			defer close(stopAsync)
			for {
				fmt.Print("You: ")
				line, err := reader.ReadString('\n')
				if err != nil {
					return err
				}
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				resp, err := runtime.Engine.Ask(context.Background(), agent.InboundMessage{
					SessionID: sessionID,
					Channel:   "cli",
					ChatID:    "direct",
					SenderID:  "user",
					Content:   line,
					CreatedAt: time.Now().UTC(),
				})
				if err != nil {
					fmt.Printf("Error: %v\n", err)
					continue
				}
				fmt.Printf("\nsquidbot: %s\n\n", resp)
			}
		},
	}
	cmd.Flags().StringVarP(&message, "message", "m", "", "Message to send")
	cmd.Flags().StringVarP(&sessionID, "session", "s", "cli:default", "Session ID")
	cmd.Flags().BoolVar(&stream, "stream", false, "Stream response chunks")
	return cmd
}

func gatewayCmd(configPath string, logger *log.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gateway",
		Short: "Start squidbot gateway (telegram + cron + heartbeat)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(configPath)
			if err != nil {
				return err
			}
			if err := config.ValidateActiveProvider(cfg); err != nil {
				return fmt.Errorf("provider setup incomplete: %w. Run `squidbot onboard`", err)
			}
			if err := config.EnsureFilesystem(cfg); err != nil {
				return err
			}
			runtime, err := app.BuildRuntime(cfg, logger)
			if err != nil {
				return err
			}
			defer runtime.Shutdown()

			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer cancel()

			fmt.Println("squidbot gateway started")
			return runtime.StartGateway(ctx)
		},
	}
	return cmd
}

func telegramCmd(configPath string) *cobra.Command {
	root := &cobra.Command{Use: "telegram", Short: "Telegram channel commands"}
	root.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Show telegram configuration status",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(configPath)
			if err != nil {
				return err
			}
			tokenSet := strings.TrimSpace(cfg.Channels.Telegram.Token) != ""
			fmt.Printf("Enabled: %v\n", cfg.Channels.Telegram.Enabled)
			fmt.Printf("Token set: %v\n", tokenSet)
			fmt.Printf("Allow list size: %d\n", len(cfg.Channels.Telegram.AllowFrom))
			return nil
		},
	})
	return root
}

func cronCmd(configPath string, logger *log.Logger) *cobra.Command {
	root := &cobra.Command{Use: "cron", Short: "Manage scheduled jobs"}
	var includeDisabled bool
	root.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List jobs",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(configPath)
			if err != nil {
				return err
			}
			store, err := storepkg.Open(cfg.Storage.DBPath)
			if err != nil {
				return err
			}
			defer store.Close()
			service := cron.NewService(store, nil, nil)
			jobs, err := service.List(context.Background(), includeDisabled)
			if err != nil {
				return err
			}
			if len(jobs) == 0 {
				fmt.Println("No jobs")
				return nil
			}
			for _, job := range jobs {
				next := ""
				if job.State.NextRunAt != nil {
					next = job.State.NextRunAt.Format(time.RFC3339)
				}
				fmt.Printf("%s\t%s\t%v\t%s\n", job.ID, job.Name, job.Enabled, next)
			}
			return nil
		},
	})
	root.PersistentFlags().BoolVarP(&includeDisabled, "all", "a", false, "Include disabled jobs")

	var name, msg, cronExpr, at string
	var every int64
	var deliver bool
	var to, channel string
	add := &cobra.Command{
		Use:   "add",
		Short: "Add a scheduled job",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(configPath)
			if err != nil {
				return err
			}
			store, err := storepkg.Open(cfg.Storage.DBPath)
			if err != nil {
				return err
			}
			defer store.Close()
			service := cron.NewService(store, nil, nil)
			job := cron.Job{
				ID:      fmt.Sprintf("job-%d", time.Now().UnixNano()),
				Name:    name,
				Enabled: true,
				Payload: cron.JobPayload{Message: msg, Deliver: deliver, Channel: channel, To: to},
			}
			switch {
			case every > 0:
				job.Schedule = cron.JobSchedule{Kind: cron.ScheduleEvery, Every: every * 1000}
			case strings.TrimSpace(cronExpr) != "":
				job.Schedule = cron.JobSchedule{Kind: cron.ScheduleCron, Expr: cronExpr}
			case strings.TrimSpace(at) != "":
				parsed, parseErr := time.Parse(time.RFC3339, at)
				if parseErr != nil {
					return parseErr
				}
				job.Schedule = cron.JobSchedule{Kind: cron.ScheduleAt, At: &parsed}
			default:
				return fmt.Errorf("provide --every, --cron, or --at")
			}
			if err := service.Put(context.Background(), job); err != nil {
				return err
			}
			fmt.Printf("Added job %s (%s)\n", job.Name, job.ID)
			return nil
		},
	}
	add.Flags().StringVarP(&name, "name", "n", "", "Job name")
	add.Flags().StringVarP(&msg, "message", "m", "", "Message payload")
	add.Flags().Int64VarP(&every, "every", "e", 0, "Run every N seconds")
	add.Flags().StringVarP(&cronExpr, "cron", "c", "", "Cron expression")
	add.Flags().StringVar(&at, "at", "", "Run once at RFC3339 time")
	add.Flags().BoolVarP(&deliver, "deliver", "d", false, "Deliver response to channel")
	add.Flags().StringVar(&channel, "channel", "telegram", "Delivery channel")
	add.Flags().StringVar(&to, "to", "", "Delivery target chat ID")
	_ = add.MarkFlagRequired("name")
	_ = add.MarkFlagRequired("message")
	root.AddCommand(add)

	root.AddCommand(&cobra.Command{
		Use:   "remove <job_id>",
		Short: "Remove a job",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(configPath)
			if err != nil {
				return err
			}
			store, err := storepkg.Open(cfg.Storage.DBPath)
			if err != nil {
				return err
			}
			defer store.Close()
			service := cron.NewService(store, nil, nil)
			return service.Remove(context.Background(), args[0])
		},
	})

	var disable bool
	enable := &cobra.Command{
		Use:   "enable <job_id>",
		Short: "Enable or disable a job",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(configPath)
			if err != nil {
				return err
			}
			store, err := storepkg.Open(cfg.Storage.DBPath)
			if err != nil {
				return err
			}
			defer store.Close()
			service := cron.NewService(store, nil, nil)
			return service.Enable(context.Background(), args[0], !disable)
		},
	}
	enable.Flags().BoolVar(&disable, "disable", false, "Disable instead of enable")
	root.AddCommand(enable)

	var force bool
	run := &cobra.Command{
		Use:   "run <job_id>",
		Short: "Run a job now",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(configPath)
			if err != nil {
				return err
			}
			if err := config.ValidateActiveProvider(cfg); err != nil {
				return fmt.Errorf("provider setup incomplete: %w. Run `squidbot onboard`", err)
			}
			runtime, err := app.BuildRuntime(cfg, logger)
			if err != nil {
				return err
			}
			defer runtime.Shutdown()
			return runtime.Cron.RunNow(context.Background(), args[0], force)
		},
	}
	run.Flags().BoolVarP(&force, "force", "f", false, "Run even if disabled")
	root.AddCommand(run)

	return root
}

func subagentsCmd(configPath string) *cobra.Command {
	root := &cobra.Command{Use: "subagents", Short: "Inspect and manage subagent runs"}
	var sessionID string
	var status string
	var limit int
	list := &cobra.Command{
		Use:   "list",
		Short: "List subagent runs",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(configPath)
			if err != nil {
				return err
			}
			store, err := storepkg.Open(cfg.Storage.DBPath)
			if err != nil {
				return err
			}
			defer store.Close()
			ctx := context.Background()
			var runs []subagent.Run
			if strings.TrimSpace(status) != "" {
				runs, err = store.ListSubagentRunsByStatus(ctx, subagent.Status(strings.TrimSpace(strings.ToLower(status))), limit)
			} else {
				runs, err = store.ListSubagentRunsBySession(ctx, strings.TrimSpace(sessionID), limit)
			}
			if err != nil {
				return err
			}
			if len(runs) == 0 {
				fmt.Println("No subagent runs")
				return nil
			}
			for _, run := range runs {
				fmt.Printf("%s\t%s\tattempt %d/%d\t%s\n", run.ID, run.Status, run.Attempt, run.MaxAttempts, run.Task)
			}
			return nil
		},
	}
	list.Flags().StringVar(&sessionID, "session", "", "Filter by session ID")
	list.Flags().StringVar(&status, "status", "", "Filter by status (queued|running|succeeded|failed|timed_out|cancelled)")
	list.Flags().IntVar(&limit, "limit", 50, "Max number of runs to return")
	root.AddCommand(list)

	show := &cobra.Command{
		Use:   "show <run_id>",
		Short: "Show one subagent run",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(configPath)
			if err != nil {
				return err
			}
			store, err := storepkg.Open(cfg.Storage.DBPath)
			if err != nil {
				return err
			}
			defer store.Close()
			run, err := store.GetSubagentRun(context.Background(), args[0])
			if err != nil {
				return err
			}
			payload, err := json.MarshalIndent(run, "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(payload))
			return nil
		},
	}
	root.AddCommand(show)

	cancel := &cobra.Command{
		Use:   "cancel <run_id>",
		Short: "Cancel a queued or running subagent run",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(configPath)
			if err != nil {
				return err
			}
			store, err := storepkg.Open(cfg.Storage.DBPath)
			if err != nil {
				return err
			}
			defer store.Close()
			ctx := context.Background()
			run, err := store.GetSubagentRun(ctx, args[0])
			if err != nil {
				return err
			}
			if run.Status.Terminal() {
				fmt.Printf("Run %s already terminal: %s\n", run.ID, run.Status)
				return nil
			}
			now := time.Now().UTC()
			if err := store.PutKV(ctx, subagent.CancelSignalNamespace, run.ID, []byte(now.Format(time.RFC3339Nano))); err != nil {
				return err
			}
			run.Status = subagent.StatusCancelled
			run.Error = "cancelled via CLI"
			run.FinishedAt = &now
			if err := store.PutSubagentRun(ctx, run); err != nil {
				return err
			}
			if err := store.AppendSubagentEvent(ctx, subagent.Event{
				RunID:     run.ID,
				Status:    subagent.StatusCancelled,
				Message:   "run cancelled via CLI",
				Attempt:   run.Attempt,
				CreatedAt: now,
			}); err != nil {
				return err
			}
			fmt.Printf("Run %s marked as cancelled\n", run.ID)
			return nil
		},
	}
	root.AddCommand(cancel)

	return root
}

func skillsCmd(configPath string) *cobra.Command {
	root := &cobra.Command{Use: "skills", Short: "Inspect and validate skill runtime state"}
	var channel string
	var asJSON bool
	list := &cobra.Command{
		Use:   "list",
		Short: "List discovered skills",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(configPath)
			if err != nil {
				return err
			}
			runtime := skills.NewManager(cfg, log.Default())
			if err := runtime.Discover(cmd.Context()); err != nil {
				return err
			}
			snapshot := runtime.Snapshot()
			if asJSON {
				raw, err := json.MarshalIndent(snapshot, "", "  ")
				if err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), string(raw))
				return nil
			}
			for _, item := range snapshot.Skills {
				decision := "enabled"
				allowed, denied := skillsPolicyOutcome(cfg, channel, item)
				if !allowed {
					decision = denied
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\tvalid=%v\ttags=%s\t%s\n", item.ID, item.Name, item.SourceKind, item.Valid, strings.Join(item.Tags, ","), decision)
				fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", item.Path)
			}
			if len(snapshot.Warnings) > 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "Warnings:")
				for _, warning := range snapshot.Warnings {
					fmt.Fprintf(cmd.OutOrStdout(), "- %s\n", warning)
				}
			}
			return nil
		},
	}
	list.Flags().StringVar(&channel, "channel", "", "Channel id for policy visibility")
	list.Flags().BoolVar(&asJSON, "json", false, "Emit JSON output")
	root.AddCommand(list)

	showJSON := false
	showQuery := ""
	showSessionID := ""
	showSubagent := false
	showMentions := []string{}
	show := &cobra.Command{
		Use:   "show <skill_id>",
		Short: "Show one skill descriptor",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(configPath)
			if err != nil {
				return err
			}
			runtime := skills.NewManager(cfg, log.Default())
			if err := runtime.Discover(cmd.Context()); err != nil {
				return err
			}
			snapshot := runtime.Snapshot()
			id := strings.ToLower(strings.TrimSpace(args[0]))
			for _, item := range snapshot.Skills {
				if strings.ToLower(item.ID) != id {
					continue
				}
				allowed, denied := skillsPolicyOutcome(cfg, channel, item)
				payload := map[string]any{
					"skill":           item,
					"channel":         channel,
					"policy_allowed":  allowed,
					"policy_decision": denied,
				}
				if strings.TrimSpace(showQuery) != "" {
					activation, err := runtime.Activate(cmd.Context(), skills.ActivationRequest{
						Query:            showQuery,
						Channel:          channel,
						SessionID:        strings.TrimSpace(showSessionID),
						IsSubagent:       showSubagent,
						ExplicitMentions: showMentions,
					})
					if err != nil {
						payload["activation_error"] = err.Error()
					} else {
						status := "unmatched"
						reason := ""
						score := 0
						matchedBy := []string{}
						breakdown := skills.ScoreBreakdown{}
						for _, ranked := range activation.Diagnostics.Ranked {
							if strings.EqualFold(ranked.ID, item.ID) {
								status = ranked.Status
								reason = ranked.Reason
								score = ranked.Score
								matchedBy = append([]string(nil), ranked.MatchedBy...)
								breakdown = ranked.Breakdown
								break
							}
						}
						payload["activation"] = map[string]any{
							"query":      showQuery,
							"status":     status,
							"reason":     reason,
							"score":      score,
							"matched_by": matchedBy,
							"breakdown":  breakdown,
							"errors":     activation.Errors,
							"warnings":   activation.Warnings,
						}
					}
				}
				if showJSON {
					raw, err := json.MarshalIndent(payload, "", "  ")
					if err != nil {
						return err
					}
					fmt.Fprintln(cmd.OutOrStdout(), string(raw))
					return nil
				}
				raw, err := json.MarshalIndent(payload, "", "  ")
				if err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), string(raw))
				return nil
			}
			return fmt.Errorf("skill %q not found", args[0])
		},
	}
	show.Flags().StringVar(&channel, "channel", "", "Channel id for policy visibility")
	show.Flags().BoolVar(&showJSON, "json", false, "Emit JSON output")
	show.Flags().StringVar(&showQuery, "query", "", "Evaluate activation scoring for this query")
	show.Flags().StringVar(&showSessionID, "session", "", "Session id used for activation diagnostics")
	show.Flags().BoolVar(&showSubagent, "subagent", false, "Evaluate routing as subagent context")
	show.Flags().StringSliceVar(&showMentions, "mention", nil, "Explicit skill mentions for activation diagnostics (repeatable)")
	root.AddCommand(show)

	var strict bool
	checkJSON := false
	check := &cobra.Command{
		Use:   "check",
		Short: "Validate all discovered skills",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(configPath)
			if err != nil {
				return err
			}
			runtime := skills.NewManager(cfg, log.Default())
			if err := runtime.Discover(cmd.Context()); err != nil {
				return err
			}
			snapshot := runtime.Snapshot()
			invalid := make([]skills.SkillDescriptor, 0)
			for _, item := range snapshot.Skills {
				if !item.Valid {
					invalid = append(invalid, item)
				}
			}
			payload := map[string]any{
				"total":    len(snapshot.Skills),
				"invalid":  len(invalid),
				"warnings": snapshot.Warnings,
			}
			if checkJSON {
				raw, err := json.MarshalIndent(payload, "", "  ")
				if err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), string(raw))
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "Skills: total=%d invalid=%d warnings=%d\n", len(snapshot.Skills), len(invalid), len(snapshot.Warnings))
				for _, item := range invalid {
					fmt.Fprintf(cmd.OutOrStdout(), "- %s (%s): %s\n", item.Name, item.ID, strings.Join(item.Errors, "; "))
				}
				for _, warning := range snapshot.Warnings {
					fmt.Fprintf(cmd.OutOrStdout(), "- warning: %s\n", warning)
				}
			}
			if strict && (len(invalid) > 0 || len(snapshot.Warnings) > 0) {
				return fmt.Errorf("skills validation failed")
			}
			if len(invalid) > 0 {
				return fmt.Errorf("found %d invalid skill(s)", len(invalid))
			}
			return nil
		},
	}
	check.Flags().BoolVar(&strict, "strict", false, "Treat warnings as failures")
	check.Flags().BoolVar(&checkJSON, "json", false, "Emit JSON output")
	root.AddCommand(check)

	reload := &cobra.Command{
		Use:   "reload",
		Short: "Force a skill index refresh",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(configPath)
			if err != nil {
				return err
			}
			runtime := skills.NewManager(cfg, log.Default())
			snapshot, err := runtime.Reload(cmd.Context())
			if err != nil {
				return err
			}
			valid := 0
			invalid := 0
			for _, item := range snapshot.Skills {
				if item.Valid {
					valid++
				} else {
					invalid++
				}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Skills reloaded: total=%d valid=%d invalid=%d warnings=%d\n", len(snapshot.Skills), valid, invalid, len(snapshot.Warnings))
			return nil
		},
	}
	root.AddCommand(reload)

	return root
}

func skillsPolicyOutcome(cfg config.Config, channel string, skill skills.SkillDescriptor) (bool, string) {
	channel = strings.ToLower(strings.TrimSpace(channel))
	matchAny := func(tokens []string) bool {
		if len(tokens) == 0 {
			return false
		}
		needle := strings.ToLower(skill.ID)
		name := strings.ToLower(skill.Name)
		for _, token := range tokens {
			token = strings.ToLower(strings.TrimSpace(token))
			if token == "" {
				continue
			}
			if token == needle || token == name {
				return true
			}
			for _, alias := range skill.Aliases {
				if token == strings.ToLower(alias) {
					return true
				}
			}
		}
		return false
	}
	if len(cfg.Skills.Policy.Allow) > 0 && !matchAny(cfg.Skills.Policy.Allow) {
		return false, "denied_by_global_allowlist"
	}
	if matchAny(cfg.Skills.Policy.Deny) {
		return false, "denied_by_global"
	}
	channelPolicy, ok := cfg.Skills.Policy.Channels[channel]
	if ok {
		if len(channelPolicy.Allow) > 0 && !matchAny(channelPolicy.Allow) {
			return false, "denied_by_channel_allowlist"
		}
		if matchAny(channelPolicy.Deny) {
			return false, "denied_by_channel"
		}
	}
	return true, "allowed"
}

func budgetCmd(configPath string) *cobra.Command {
	root := &cobra.Command{Use: "budget", Short: "Inspect and manage token safety budgets"}
	root.AddCommand(&cobra.Command{
		Use:   "show",
		Short: "Show effective token safety settings and usage counters",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(configPath)
			if err != nil {
				return err
			}
			store, err := storepkg.Open(cfg.Storage.DBPath)
			if err != nil {
				return err
			}
			defer store.Close()
			ctx := context.Background()
			settings := effectiveTokenSafetySettings(ctx, cfg, store)
			global, err := store.GetBudgetCounter(ctx, "global")
			if err != nil {
				return err
			}
			session := budget.Counter{}
			sessionID := "cli:default"
			session, _ = store.GetBudgetCounter(ctx, "session:"+sessionID)
			payload := map[string]any{
				"settings": settings,
				"usage": map[string]any{
					"global":  global,
					"session": session,
				},
			}
			raw, err := json.MarshalIndent(payload, "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(raw))
			return nil
		},
	})

	var scope string
	var tokens uint64
	var soft int
	setLimit := &cobra.Command{
		Use:   "set-limit",
		Short: "Set hard limit (and optional soft threshold) for a scope",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(configPath)
			if err != nil {
				return err
			}
			store, err := storepkg.Open(cfg.Storage.DBPath)
			if err != nil {
				return err
			}
			defer store.Close()
			ctx := context.Background()
			settings := effectiveTokenSafetySettings(ctx, cfg, store)
			switch strings.TrimSpace(strings.ToLower(scope)) {
			case "global":
				settings.GlobalHardLimitTokens = tokens
				if cmd.Flags().Changed("soft-threshold") {
					settings.GlobalSoftThresholdPct = soft
				}
			case "session":
				settings.SessionHardLimitTokens = tokens
				if cmd.Flags().Changed("soft-threshold") {
					settings.SessionSoftThresholdPct = soft
				}
			case "subagent":
				settings.SubagentRunHardLimitTokens = tokens
				if cmd.Flags().Changed("soft-threshold") {
					settings.SubagentRunSoftThresholdPct = soft
				}
			default:
				return fmt.Errorf("unsupported scope %q (use global|session|subagent)", scope)
			}
			if err := putTokenSafetyOverride(ctx, store, settings); err != nil {
				return err
			}
			fmt.Println("Token safety limit updated")
			return nil
		},
	}
	setLimit.Flags().StringVar(&scope, "scope", "global", "Scope: global|session|subagent")
	setLimit.Flags().Uint64Var(&tokens, "tokens", 0, "Hard token limit")
	setLimit.Flags().IntVar(&soft, "soft-threshold", 0, "Soft warning threshold percent (optional)")
	_ = setLimit.MarkFlagRequired("scope")
	_ = setLimit.MarkFlagRequired("tokens")
	root.AddCommand(setLimit)

	root.AddCommand(&cobra.Command{
		Use:   "set-mode <mode>",
		Short: "Set token safety mode (hybrid|soft|hard)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(configPath)
			if err != nil {
				return err
			}
			store, err := storepkg.Open(cfg.Storage.DBPath)
			if err != nil {
				return err
			}
			defer store.Close()
			ctx := context.Background()
			settings := effectiveTokenSafetySettings(ctx, cfg, store)
			settings.Mode = budget.Mode(budget.NormalizeMode(args[0]))
			if err := putTokenSafetyOverride(ctx, store, settings); err != nil {
				return err
			}
			fmt.Printf("Token safety mode set to %s\n", settings.Mode)
			return nil
		},
	})

	root.AddCommand(&cobra.Command{
		Use:   "enable",
		Short: "Enable token safety enforcement",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(configPath)
			if err != nil {
				return err
			}
			store, err := storepkg.Open(cfg.Storage.DBPath)
			if err != nil {
				return err
			}
			defer store.Close()
			ctx := context.Background()
			settings := effectiveTokenSafetySettings(ctx, cfg, store)
			settings.Enabled = true
			if err := putTokenSafetyOverride(ctx, store, settings); err != nil {
				return err
			}
			fmt.Println("Token safety enabled")
			return nil
		},
	})

	root.AddCommand(&cobra.Command{
		Use:   "disable",
		Short: "Disable token safety enforcement",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(configPath)
			if err != nil {
				return err
			}
			store, err := storepkg.Open(cfg.Storage.DBPath)
			if err != nil {
				return err
			}
			defer store.Close()
			ctx := context.Background()
			settings := effectiveTokenSafetySettings(ctx, cfg, store)
			settings.Enabled = false
			if err := putTokenSafetyOverride(ctx, store, settings); err != nil {
				return err
			}
			fmt.Println("Token safety disabled")
			return nil
		},
	})
	return root
}

func tokenSafetySettingsFromConfig(cfg config.Config) budget.Settings {
	return budget.Settings{
		Enabled:                     cfg.Runtime.TokenSafety.Enabled,
		Mode:                        budget.Mode(budget.NormalizeMode(cfg.Runtime.TokenSafety.Mode)),
		GlobalHardLimitTokens:       cfg.Runtime.TokenSafety.GlobalHardLimitTokens,
		GlobalSoftThresholdPct:      cfg.Runtime.TokenSafety.GlobalSoftThresholdPct,
		SessionHardLimitTokens:      cfg.Runtime.TokenSafety.SessionHardLimitTokens,
		SessionSoftThresholdPct:     cfg.Runtime.TokenSafety.SessionSoftThresholdPct,
		SubagentRunHardLimitTokens:  cfg.Runtime.TokenSafety.SubagentRunHardLimitTokens,
		SubagentRunSoftThresholdPct: cfg.Runtime.TokenSafety.SubagentRunSoftThresholdPct,
		EstimateOnMissingUsage:      cfg.Runtime.TokenSafety.EstimateOnMissingUsage,
		EstimateCharsPerToken:       cfg.Runtime.TokenSafety.EstimateCharsPerToken,
		TrustedWriters:              append([]string(nil), cfg.Runtime.TokenSafety.TrustedWriters...),
		ReservationTTLSec:           300,
	}.Normalized()
}

func effectiveTokenSafetySettings(ctx context.Context, cfg config.Config, store *storepkg.Store) budget.Settings {
	settings := tokenSafetySettingsFromConfig(cfg)
	if store == nil {
		return settings
	}
	override, err := store.GetTokenSafetyOverride(ctx)
	if err == nil {
		return override.Settings.Normalized()
	}
	return settings
}

func putTokenSafetyOverride(ctx context.Context, store *storepkg.Store, settings budget.Settings) error {
	if store == nil {
		return fmt.Errorf("store is required")
	}
	return store.PutTokenSafetyOverride(ctx, budget.TokenSafetyOverride{
		Settings:  settings.Normalized(),
		UpdatedAt: time.Now().UTC(),
		Version:   1,
	})
}

func doctorCmd(configPath string) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Run configuration and dependency checks",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(configPath)
			if err != nil {
				return err
			}
			problems := []string{}
			if err := config.ValidateActiveProvider(cfg); err != nil {
				problems = append(problems, err.Error())
			}
			if cfg.Channels.Telegram.Enabled && strings.TrimSpace(cfg.Channels.Telegram.Token) == "" {
				problems = append(problems, "Telegram enabled but token missing")
			}
			if cfg.Features.ChannelsWave1 {
				slackCfg := cfg.Channels.Registry["slack"]
				if slackCfg.Enabled {
					if strings.TrimSpace(slackCfg.Token) == "" {
						problems = append(problems, "channel \"slack\" enabled but token missing")
					}
					if strings.TrimSpace(slackCfg.Metadata["listen_addr"]) == "" {
						problems = append(problems, "channel \"slack\" enabled but metadata.listen_addr missing")
					}
				}
				discordCfg := cfg.Channels.Registry["discord"]
				if discordCfg.Enabled {
					if strings.TrimSpace(discordCfg.Token) == "" {
						problems = append(problems, "channel \"discord\" enabled but token missing")
					}
					if strings.TrimSpace(discordCfg.Metadata["listen_addr"]) == "" {
						problems = append(problems, "channel \"discord\" enabled but metadata.listen_addr missing")
					}
					if strings.TrimSpace(discordCfg.Metadata["public_key"]) == "" {
						problems = append(problems, "channel \"discord\" enabled but metadata.public_key missing")
					}
				}
				webchatCfg := cfg.Channels.Registry["webchat"]
				if webchatCfg.Enabled && strings.TrimSpace(webchatCfg.Metadata["listen_addr"]) == "" {
					problems = append(problems, "channel \"webchat\" enabled but metadata.listen_addr missing")
				}
			}
			whatsAppCfg := cfg.Channels.Registry["whatsapp"]
			if whatsAppCfg.Enabled && (strings.TrimSpace(whatsAppCfg.Token) != "" || strings.TrimSpace(whatsAppCfg.Metadata["listen_addr"]) != "") {
				if strings.TrimSpace(whatsAppCfg.Token) == "" {
					problems = append(problems, "channel \"whatsapp\" native mode requires token")
				}
				if strings.TrimSpace(whatsAppCfg.Metadata["listen_addr"]) == "" {
					problems = append(problems, "channel \"whatsapp\" native mode requires metadata.listen_addr")
				}
				if strings.TrimSpace(whatsAppCfg.Metadata["phone_number_id"]) == "" {
					problems = append(problems, "channel \"whatsapp\" native mode requires metadata.phone_number_id")
				}
			}
			if cfg.Features.Streaming {
				caps := cfg.PrimaryProvider
				_, providerCfg := caps()
				if strings.TrimSpace(providerCfg.Model) == "" && strings.TrimSpace(cfg.Agents.Defaults.Model) == "" {
					problems = append(problems, "streaming enabled but provider model is empty")
				}
			}
			if cfg.Features.Plugins || cfg.Runtime.Plugins.Enabled {
				if len(cfg.Runtime.Plugins.Paths) == 0 {
					problems = append(problems, "plugins enabled but runtime.plugins.paths is empty")
				}
			}
			if cfg.Features.MetricsHTTP || cfg.Runtime.MetricsHTTP.Enabled {
				if strings.TrimSpace(cfg.Runtime.MetricsHTTP.ListenAddr) == "" {
					problems = append(problems, "metrics http enabled but listenAddr missing")
				}
			}
			if cfg.Features.SemanticMemory || cfg.Memory.Semantic.Enabled {
				if cfg.Memory.Semantic.TopKCandidates <= 0 {
					problems = append(problems, "semantic memory topKCandidates must be > 0")
				}
				if cfg.Memory.Semantic.RerankTopK <= 0 {
					problems = append(problems, "semantic memory rerankTopK must be > 0")
				}
			}
			if cfg.Runtime.Federation.Enabled {
				if strings.TrimSpace(cfg.Runtime.Federation.ListenAddr) == "" {
					problems = append(problems, "federation enabled but runtime.federation.listenAddr missing")
				}
				for _, peer := range cfg.Runtime.Federation.Peers {
					if !peer.Enabled {
						continue
					}
					if strings.TrimSpace(peer.ID) == "" {
						problems = append(problems, "federation peer enabled but id missing")
						continue
					}
					if strings.TrimSpace(peer.BaseURL) == "" {
						problems = append(problems, fmt.Sprintf("federation peer %q enabled but baseUrl missing", peer.ID))
					}
					if strings.TrimSpace(peer.AuthToken) == "" {
						problems = append(problems, fmt.Sprintf("federation peer %q enabled but authToken missing", peer.ID))
					}
				}
			}
			workspace := config.WorkspacePath(cfg)
			required := []string{
				filepath.Join(workspace, "AGENTS.md"),
				filepath.Join(workspace, "SOUL.md"),
				filepath.Join(workspace, "USER.md"),
				filepath.Join(workspace, "TOOLS.md"),
				filepath.Join(workspace, "HEARTBEAT.md"),
				filepath.Join(workspace, "memory", "MEMORY.md"),
			}
			for _, path := range required {
				if _, statErr := os.Stat(path); statErr != nil {
					problems = append(problems, "missing workspace file: "+path)
				}
			}
			if _, readErr := os.ReadFile(filepath.Join(workspace, "HEARTBEAT.md")); readErr != nil {
				problems = append(problems, "heartbeat file not readable: "+readErr.Error())
			}
			mem := memory.NewManager(cfg)
			if err := mem.EnsureIndex(cmd.Context()); err != nil {
				problems = append(problems, "memory index unavailable: "+err.Error())
			}
			if cfg.Features.Plugins || cfg.Runtime.Plugins.Enabled {
				pluginRuntime := plugins.NewManager(cfg, log.Default())
				if err := pluginRuntime.Discover(cmd.Context()); err != nil {
					problems = append(problems, "plugin discovery failed: "+err.Error())
				}
				_ = pluginRuntime.Close()
			}
			skillsRuntime := skills.NewManager(cfg, log.Default())
			if err := skillsRuntime.Discover(cmd.Context()); err != nil {
				problems = append(problems, "skills discovery failed: "+err.Error())
			}
			snapshot := skillsRuntime.Snapshot()
			valid := 0
			invalid := 0
			zipCount := 0
			for _, skill := range snapshot.Skills {
				if skill.SourceKind == "zip" {
					zipCount++
				}
				if skill.Valid {
					valid++
				} else {
					invalid++
				}
			}
			fmt.Printf("Skills discovered: total=%d valid=%d invalid=%d zip=%d enabled=%v\n", len(snapshot.Skills), valid, invalid, zipCount, cfg.Skills.Enabled)
			for _, warning := range snapshot.Warnings {
				fmt.Printf("Skill warning: %s\n", warning)
			}
			if len(problems) == 0 {
				fmt.Println("Doctor checks passed")
				return nil
			}
			fmt.Println("Doctor found issues:")
			for _, p := range problems {
				fmt.Println("-", p)
			}
			return fmt.Errorf("doctor checks failed")
		},
	}
}
