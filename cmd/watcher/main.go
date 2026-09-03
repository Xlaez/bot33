package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/xlaez/bot33/internal/alert"
	"github.com/xlaez/bot33/internal/chain"
	"github.com/xlaez/bot33/internal/config"
	"github.com/xlaez/bot33/internal/discover"
	"github.com/xlaez/bot33/internal/enrich"
	"github.com/xlaez/bot33/internal/ingest"
	"github.com/xlaez/bot33/internal/store"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	cfg, err := config.Load()
	if err != nil {
		log.Error("config", "err", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	st, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Error("store", "err", err)
		os.Exit(1)
	}
	defer st.Close()

	if _, err := os.Stat(cfg.WalletsSeedPath); err == nil {
		n, err := st.LoadSeedFile(ctx, cfg.WalletsSeedPath)
		if err != nil {
			log.Error("load seed", "err", err)
			os.Exit(1)
		}
		log.Info("seed loaded", "count", n, "path", cfg.WalletsSeedPath)
	}

	client, err := chain.Dial(ctx, cfg.RHHTTPURL, cfg.RHWSURL, cfg.ChainID)
	if err != nil {
		log.Error("chain dial", "err", err)
		os.Exit(1)
	}
	defer client.Close()

	tg := alert.NewTelegram(cfg.TelegramBotToken, cfg.TelegramChatID)
	en := enrich.New(client.HTTP)
	watcher := ingest.New(client.HTTP, st, en, tg, log, cfg.AlertOnSell, cfg.LogPollInterval, cfg.StartBlockLag)
	scorer := discover.New(st, log, cfg.DiscoveryMinScore, cfg.DiscoveryMinTrades, cfg.DiscoveryInterval)

	errCh := make(chan error, 2)
	go func() { errCh <- watcher.Run(ctx) }()
	go func() { errCh <- scorer.Run(ctx) }()

	log.Info("watcher started", "chain_id", cfg.ChainID, "telegram", tg.Enabled())
	select {
	case <-ctx.Done():
		log.Info("shutting down")
	case err := <-errCh:
		if err != nil && err != context.Canceled {
			log.Error("fatal", "err", err)
			os.Exit(1)
		}
	}
}
