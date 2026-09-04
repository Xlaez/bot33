package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/xlaez/bot33/internal/alert"
	"github.com/xlaez/bot33/internal/chain"
	"github.com/xlaez/bot33/internal/config"
	"github.com/xlaez/bot33/internal/meme"
	"github.com/xlaez/bot33/internal/store"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	log.Info("starting meme watcher")

	cfg, err := config.Load()
	if err != nil {
		log.Error("config", "err", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	dbCtx, dbCancel := context.WithTimeout(ctx, 15*time.Second)
	defer dbCancel()
	st, err := store.Open(dbCtx, cfg.DatabaseURL)
	if err != nil {
		log.Error("store", "err", err)
		os.Exit(1)
	}
	defer st.Close()

	rpcCtx, rpcCancel := context.WithTimeout(ctx, 20*time.Second)
	defer rpcCancel()
	client, err := chain.Dial(rpcCtx, cfg.RHHTTPURL, cfg.RHWSURL, cfg.ChainID)
	if err != nil {
		log.Error("chain dial", "err", err)
		os.Exit(1)
	}
	defer client.Close()

	tg := alert.NewTelegram(cfg.TelegramBotToken, cfg.TelegramMemeChatID)
	memeKey := cfg.ExecutorPrivateKey
	if memeKey == "" && len(cfg.ExecutorPrivateKeys) > 0 {
		memeKey = cfg.ExecutorPrivateKeys[0]
	}
	buyer, err := meme.NewBuyer(client.HTTP, st, log, tg, memeKey, cfg.ChainID)
	if err != nil {
		log.Error("meme buyer", "err", err)
		os.Exit(1)
	}
	w := meme.NewWatcher(client.HTTP, st, log, tg, buyer, cfg.MemePollInterval, cfg.StartBlockLag)

	errCh := make(chan error, 2)
	go func() { errCh <- buyer.Run(ctx) }()
	go func() { errCh <- w.Run(ctx) }()

	log.Info("meme watcher ready",
		"chain_id", cfg.ChainID,
		"telegram", tg.Enabled(),
		"chat_configured", cfg.TelegramMemeChatID != "",
		"poll", cfg.MemePollInterval.String(),
		"buyer", buyer.HasSigner(),
		"signer", buyer.SignerAddress(),
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
