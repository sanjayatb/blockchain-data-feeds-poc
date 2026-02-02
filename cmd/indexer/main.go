package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"blockchain-data-feeds-poc/internal/config"
	"blockchain-data-feeds-poc/internal/decode"
	"blockchain-data-feeds-poc/internal/extract"
	"blockchain-data-feeds-poc/internal/mempool"
	"blockchain-data-feeds-poc/internal/metrics"
	"blockchain-data-feeds-poc/internal/rpc"
	"blockchain-data-feeds-poc/internal/store"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	switch os.Args[1] {
	case "index":
		handleIndex(ctx, logger, os.Args[2:])
	case "db":
		handleDB(ctx, logger, os.Args[2:])
	default:
		usage()
		os.Exit(1)
	}
}

func handleIndex(ctx context.Context, logger *slog.Logger, args []string) {
	if len(args) < 1 {
		indexUsage()
		os.Exit(1)
	}

	switch args[0] {
	case "backfill":
		fs := flag.NewFlagSet("backfill", flag.ExitOnError)
		configPath := fs.String("config", "", "Path to config YAML")
		from := fs.Uint64("from", 0, "Override start block")
		to := fs.Uint64("to", 0, "Override end block")
		_ = fs.Parse(args[1:])
		if *configPath == "" {
			fmt.Fprintln(os.Stderr, "--config is required")
			os.Exit(1)
		}
		cfg := mustLoadConfig(*configPath)
		deps := mustBuildDeps(ctx, logger, cfg)
		defer deps.Store.Close()
		var fromPtr *uint64
		var toPtr *uint64
		if *from > 0 {
			fromPtr = from
		}
		if *to > 0 {
			toPtr = to
		}
		if err := extract.Backfill(ctx, cfg, deps, fromPtr, toPtr); err != nil {
			logger.Error("backfill failed", slog.String("error", err.Error()))
			os.Exit(1)
		}
	case "follow":
		fs := flag.NewFlagSet("follow", flag.ExitOnError)
		configPath := fs.String("config", "", "Path to config YAML")
		_ = fs.Parse(args[1:])
		if *configPath == "" {
			fmt.Fprintln(os.Stderr, "--config is required")
			os.Exit(1)
		}
		cfg := mustLoadConfig(*configPath)
		deps := mustBuildDeps(ctx, logger, cfg)
		defer deps.Store.Close()
		if err := extract.Follow(ctx, cfg, deps); err != nil {
			logger.Error("follow failed", slog.String("error", err.Error()))
			os.Exit(1)
		}
	case "mempool":
		fs := flag.NewFlagSet("mempool", flag.ExitOnError)
		configPath := fs.String("config", "", "Path to config YAML")
		decodeInput := fs.Bool("decode-input", false, "Decode transaction input using ABI")
		track := fs.Bool("track", false, "Track lifecycle (mined/replaced/dropped)")
		droppedTTL := fs.String("dropped-ttl", "", "Dropped TTL duration (e.g. 10m)")
		pendingWorkers := fs.Int("pending-workers", 0, "Pending tx fetch workers")
		receiptWorkers := fs.Int("receipt-workers", 0, "Receipt workers")
		_ = fs.Parse(args[1:])
		if *configPath == "" {
			fmt.Fprintln(os.Stderr, "--config is required")
			os.Exit(1)
		}
		cfg := mustLoadConfig(*configPath)
		svc, st := mustBuildMempoolService(ctx, logger, cfg)
		defer st.Close()
		if err := svc.Run(ctx, *decodeInput, *track, *droppedTTL, *pendingWorkers, *receiptWorkers); err != nil {
			logger.Error("mempool failed", slog.String("error", err.Error()))
			os.Exit(1)
		}
	default:
		indexUsage()
		os.Exit(1)
	}
}

func handleDB(ctx context.Context, logger *slog.Logger, args []string) {
	if len(args) < 1 {
		dbUsage()
		os.Exit(1)
	}

	switch args[0] {
	case "migrate":
		fs := flag.NewFlagSet("migrate", flag.ExitOnError)
		configPath := fs.String("config", "", "Path to config YAML")
		_ = fs.Parse(args[1:])
		if *configPath == "" {
			fmt.Fprintln(os.Stderr, "--config is required")
			os.Exit(1)
		}
		cfg := mustLoadConfig(*configPath)
		st, err := store.New(cfg.Database)
		if err != nil {
			logger.Error("open store failed", slog.String("error", err.Error()))
			os.Exit(1)
		}
		defer st.Close()
		if err := st.Migrate(ctx); err != nil {
			logger.Error("migrate failed", slog.String("error", err.Error()))
			os.Exit(1)
		}
		logger.Info("migration complete")
	default:
		dbUsage()
		os.Exit(1)
	}
}

func mustLoadConfig(path string) *config.Config {
	cfg, err := config.Load(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	return cfg
}

func mustBuildDeps(ctx context.Context, logger *slog.Logger, cfg *config.Config) extract.Deps {
	st, err := store.New(cfg.Database)
	if err != nil {
		logger.Error("open store failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	if err := st.Migrate(ctx); err != nil {
		logger.Error("migrate failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	contracts := make([]decode.ContractSpec, 0, len(cfg.Contracts))
	for _, c := range cfg.Contracts {
		contracts = append(contracts, decode.ContractSpec{
			Name:    c.Name,
			Address: c.Address,
			ABIPath: c.ABIPath,
			Events:  c.Events,
		})
	}
	reg, err := decode.LoadRegistry(contracts)
	if err != nil {
		logger.Error("load registry failed", slog.String("error", err.Error()))
		os.Exit(1)
	}

	rpcClient := rpc.NewClient(cfg.Chain.RPCURL, cfg.Indexer.Concurrency, cfg.Indexer.RateLimitPerSecond, logger)

	m := &metrics.Metrics{}
	m.StartLogger(ctx, logger, time.Duration(cfg.Indexer.MetricsIntervalSeconds)*time.Second)

	return extract.Deps{
		RPC:     rpcClient,
		Store:   st,
		Decoder: reg,
		Logger:  logger,
		Metrics: m,
		ChainID: cfg.Chain.ChainID,
	}
}

func mustBuildMempoolService(ctx context.Context, logger *slog.Logger, cfg *config.Config) (*mempool.Service, store.Store) {
	st, err := store.New(cfg.Database)
	if err != nil {
		logger.Error("open store failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	if err := st.Migrate(ctx); err != nil {
		logger.Error("migrate failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	contracts := make([]decode.ContractSpec, 0, len(cfg.Contracts))
	for _, c := range cfg.Contracts {
		contracts = append(contracts, decode.ContractSpec{
			Name:    c.Name,
			Address: c.Address,
			ABIPath: c.ABIPath,
			Events:  c.Events,
		})
	}
	reg, err := decode.LoadRegistry(contracts)
	if err != nil {
		logger.Error("load registry failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	rpcClient := rpc.NewClient(cfg.Mempool.HTTPURL, cfg.Mempool.PendingWorkers, cfg.Indexer.RateLimitPerSecond, logger)
	svc := mempool.NewService(cfg, rpcClient, reg, st, logger)
	return svc, st
}

func usage() {
	fmt.Println("Usage:")
	fmt.Println("  indexer index backfill --config <path> [--from N] [--to N]")
	fmt.Println("  indexer index follow --config <path>")
	fmt.Println("  indexer index mempool --config <path> [--decode-input] [--track] [--dropped-ttl 10m] [--pending-workers N] [--receipt-workers N]")
	fmt.Println("  indexer db migrate --config <path>")
}

func indexUsage() {
	fmt.Println("Usage:")
	fmt.Println("  indexer index backfill --config <path> [--from N] [--to N]")
	fmt.Println("  indexer index follow --config <path>")
	fmt.Println("  indexer index mempool --config <path> [--decode-input] [--track] [--dropped-ttl 10m] [--pending-workers N] [--receipt-workers N]")
}

func dbUsage() {
	fmt.Println("Usage:")
	fmt.Println("  indexer db migrate --config <path>")
}
