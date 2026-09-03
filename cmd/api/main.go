package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/gofiber/fiber/v2"
	"github.com/xlaez/bot33/internal/config"
	"github.com/xlaez/bot33/internal/store"
	"github.com/xlaez/bot33/internal/wallet"
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

	app := fiber.New(fiber.Config{AppName: "bot33-api"})
	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"ok": true, "chain_id": cfg.ChainID})
	})
	app.Get("/wallets", func(c *fiber.Ctx) error {
		rows, err := st.ListWallets(c.Context())
		if err != nil {
			return fiber.NewError(fiber.StatusInternalServerError, err.Error())
		}
		return c.JSON(rows)
	})
	app.Get("/wallets/watch", func(c *fiber.Ctx) error {
		rows, err := st.ListActiveWatchSet(c.Context())
		if err != nil {
			return fiber.NewError(fiber.StatusInternalServerError, err.Error())
		}
		return c.JSON(rows)
	})
	app.Post("/wallets", func(c *fiber.Ctx) error {
		var body wallet.Record
		if err := c.BodyParser(&body); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, err.Error())
		}
		body.Address = wallet.NormalizeAddress(body.Address)
		if body.Address == "" {
			return fiber.NewError(fiber.StatusBadRequest, "address required")
		}
		if body.Source == "" {
			body.Source = wallet.SourceCurated
		}
		body.Active = true
		if err := st.UpsertWallet(c.Context(), body); err != nil {
			return fiber.NewError(fiber.StatusInternalServerError, err.Error())
		}
		return c.Status(fiber.StatusCreated).JSON(body)
	})
	app.Post("/wallets/seed", func(c *fiber.Ctx) error {
		n, err := st.LoadSeedFile(c.Context(), cfg.WalletsSeedPath)
		if err != nil {
			return fiber.NewError(fiber.StatusInternalServerError, err.Error())
		}
		return c.JSON(fiber.Map{"loaded": n, "path": cfg.WalletsSeedPath})
	})

	go func() {
		<-ctx.Done()
		_ = app.Shutdown()
	}()

	log.Info("api listening", "addr", cfg.HTTPAddr)
	if err := app.Listen(cfg.HTTPAddr); err != nil {
		log.Error("api stopped", "err", err)
		os.Exit(1)
	}
}
