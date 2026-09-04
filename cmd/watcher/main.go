package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/xlaez/bot33/internal/alert"
	"github.com/xlaez/bot33/internal/chain"
	"github.com/xlaez/bot33/internal/config"
	"github.com/xlaez/bot33/internal/discover"
	"github.com/xlaez/bot33/internal/enrich"
	"github.com/xlaez/bot33/internal/execute"
	"github.com/xlaez/bot33/internal/ingest"
	"github.com/xlaez/bot33/internal/marketplace"
	"github.com/xlaez/bot33/internal/store"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	log.Info("starting watcher")

	cfg, err := config.Load()
	if err != nil {
		log.Error("config", "err", err)
		os.Exit(1)
	}
	log.Info("config loaded", "root", cfg.RootDir, "chain_id", cfg.ChainID, "rpc", cfg.RHHTTPURL)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	dbCtx, dbCancel := context.WithTimeout(ctx, 15*time.Second)
	defer dbCancel()
	log.Info("connecting database")
	st, err := store.Open(dbCtx, cfg.DatabaseURL)
	if err != nil {
		log.Error("store", "err", err)
		os.Exit(1)
	}
	defer st.Close()
	log.Info("database ready")

	if _, err := os.Stat(cfg.WalletsSeedPath); err == nil {
		n, err := st.LoadSeedFile(ctx, cfg.WalletsSeedPath)
		if err != nil {
			log.Error("load seed", "err", err)
			os.Exit(1)
		}
		log.Info("seed loaded", "count", n, "path", cfg.WalletsSeedPath)
	} else {
		log.Warn("seed file not found", "path", cfg.WalletsSeedPath)
	}

	rpcCtx, rpcCancel := context.WithTimeout(ctx, 20*time.Second)
	defer rpcCancel()
	log.Info("dialing rpc", "url", cfg.RHHTTPURL)
	client, err := chain.Dial(rpcCtx, cfg.RHHTTPURL, cfg.RHWSURL, cfg.ChainID)
	if err != nil {
		log.Error("chain dial", "err", err)
		os.Exit(1)
	}
	defer client.Close()
	log.Info("rpc ready")

	tg := alert.NewTelegram(cfg.TelegramBotToken, cfg.TelegramChatID)
	en := enrich.New(client.HTTP)
	engine, err := execute.New(client.HTTP, st, log, tg, cfg.ExecutorPrivateKey, cfg.ChainID)
	if err != nil {
		log.Error("executor", "err", err)
		os.Exit(1)
	}
	watcher := ingest.New(client.HTTP, st, en, tg, log, cfg.AlertOnSell, cfg.LogPollInterval, cfg.StartBlockLag)
	watcher.SetOnMint(func(collection common.Address, walletAddr, label, txHash string) {
		settings, err := st.GetSettings(ctx)
		if err != nil || !settings.AutoCopyMint {
			return
		}
		log.Info("copy-mint signal", "collection", collection.Hex(), "wallet", walletAddr, "tx", txHash)
		engine.Enqueue(execute.Job{
			Source:     "copy",
			Collection: collection,
			Quantity:   settings.MintQuantity,
			SignalTx:   txHash,
			Label:      label,
		})
	})
	scorer := discover.New(st, log, discover.Options{
		MinScore:  cfg.DiscoveryMinScore,
		MinTrades: cfg.DiscoveryMinTrades,
		TopN:      cfg.DiscoveryTopN,
		Window:    cfg.DiscoveryWindow,
		Interval:  cfg.DiscoveryInterval,
	})
	mkt := marketplace.New(client.HTTP, st, log, cfg.MarketplaceEnabled, cfg.LogPollInterval, cfg.StartBlockLag)

	errCh := make(chan error, 4)
	go func() { errCh <- watcher.Run(ctx) }()
	go func() { errCh <- scorer.Run(ctx) }()
	go func() { errCh <- engine.Run(ctx) }()
	go func() { errCh <- mkt.Run(ctx) }()

	log.Info("watcher started",
		"chain_id", cfg.ChainID,
		"telegram", tg.Enabled(),
		"poll", cfg.LogPollInterval.String(),
		"executor", engine.HasSigner(),
		"signer", engine.SignerAddress(),
		"discovery_window", cfg.DiscoveryWindow.String(),
		"discovery_top_n", cfg.DiscoveryTopN,
		"marketplace", cfg.MarketplaceEnabled,
	)
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
