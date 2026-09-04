package main

import (
	"context"
	"fmt"
	"log/slog"
	"math/big"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/xlaez/bot33/internal/alert"
	"github.com/xlaez/bot33/internal/chain"
	"github.com/xlaez/bot33/internal/config"
	"github.com/xlaez/bot33/internal/execute"
	"github.com/xlaez/bot33/internal/meme"
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

	if path := strings.TrimSpace(os.Getenv("COLLECTIONS_PATH")); path != "" {
		if n, err := st.LoadCollectionsFile(ctx, path); err != nil {
			log.Warn("collections seed", "err", err)
		} else {
			log.Info("collections loaded", "count", n)
		}
	} else if _, err := os.Stat("configs/collections.yaml"); err == nil {
		if n, err := st.LoadCollectionsFile(ctx, "configs/collections.yaml"); err == nil {
			log.Info("collections loaded", "count", n)
		}
	}

	app := fiber.New(fiber.Config{
		AppName:      "bot33",
		ErrorHandler: apiError,
	})
	app.Use(logger.New())
	app.Use(cors.New(cors.Config{
		AllowOrigins: "*",
		AllowMethods: "GET,POST,PATCH,DELETE,OPTIONS",
		AllowHeaders: "Origin, Content-Type, Accept",
	}))

	api := app.Group("/api")
	api.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"ok": true, "chain_id": cfg.ChainID})
	})
	api.Get("/status", func(c *fiber.Ctx) error {
		stats, err := st.Stats(c.Context())
		if err != nil {
			return fiber.NewError(fiber.StatusInternalServerError, err.Error())
		}
		stats.ChainID = cfg.ChainID
		return c.JSON(stats)
	})

	api.Get("/wallets", func(c *fiber.Ctx) error {
		rows, err := st.ListWallets(c.Context())
		if err != nil {
			return fiber.NewError(fiber.StatusInternalServerError, err.Error())
		}
		if rows == nil {
			rows = []wallet.Record{}
		}
		return c.JSON(rows)
	})
	api.Get("/wallets/watch", func(c *fiber.Ctx) error {
		rows, err := st.ListActiveWatchSet(c.Context())
		if err != nil {
			return fiber.NewError(fiber.StatusInternalServerError, err.Error())
		}
		if rows == nil {
			rows = []wallet.Record{}
		}
		return c.JSON(rows)
	})
	api.Post("/wallets", func(c *fiber.Ctx) error {
		var body wallet.Record
		if err := c.BodyParser(&body); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, err.Error())
		}
		body.Address = wallet.NormalizeAddress(body.Address)
		if body.Address == "" || !strings.HasPrefix(body.Address, "0x") || len(body.Address) != 42 {
			return fiber.NewError(fiber.StatusBadRequest, "valid 0x address required")
		}
		if body.Source == "" {
			body.Source = wallet.SourceCurated
		}
		body.Active = true
		if len(body.Tags) == 0 {
			body.Tags = []string{"manual"}
		}
		if err := st.UpsertWallet(c.Context(), body); err != nil {
			return fiber.NewError(fiber.StatusInternalServerError, err.Error())
		}
		return c.Status(fiber.StatusCreated).JSON(body)
	})
	api.Patch("/wallets/:address", func(c *fiber.Ctx) error {
		var body struct {
			Active *bool  `json:"active"`
			Label  string `json:"label"`
		}
		if err := c.BodyParser(&body); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, err.Error())
		}
		addr := wallet.NormalizeAddress(c.Params("address"))
		if body.Active != nil {
			if err := st.SetWalletActive(c.Context(), addr, *body.Active); err != nil {
				return fiber.NewError(fiber.StatusNotFound, err.Error())
			}
		}
		if strings.TrimSpace(body.Label) != "" {
			if err := st.UpsertWallet(c.Context(), wallet.Record{
				Address: addr,
				Label:   body.Label,
				Source:  wallet.SourceCurated,
				Active:  true,
			}); err != nil {
				return fiber.NewError(fiber.StatusInternalServerError, err.Error())
			}
		}
		return c.SendStatus(fiber.StatusNoContent)
	})
	api.Delete("/wallets/:address", func(c *fiber.Ctx) error {
		if err := st.DeleteWallet(c.Context(), c.Params("address")); err != nil {
			return fiber.NewError(fiber.StatusNotFound, err.Error())
		}
		return c.SendStatus(fiber.StatusNoContent)
	})
	api.Post("/wallets/seed", func(c *fiber.Ctx) error {
		n, err := st.LoadSeedFile(c.Context(), cfg.WalletsSeedPath)
		if err != nil {
			return fiber.NewError(fiber.StatusInternalServerError, err.Error())
		}
		return c.JSON(fiber.Map{"loaded": n, "path": cfg.WalletsSeedPath})
	})

	api.Get("/trades", func(c *fiber.Ctx) error {
		scope := strings.ToLower(strings.TrimSpace(c.Query("scope", "watched")))
		watchedOnly := scope != "all"
		rows, err := st.ListTrades(c.Context(), c.QueryInt("limit", 100), watchedOnly)
		if err != nil {
			return fiber.NewError(fiber.StatusInternalServerError, err.Error())
		}
		return c.JSON(rows)
	})

	api.Get("/collections", func(c *fiber.Ctx) error {
		rows, err := st.ListCollections(c.Context())
		if err != nil {
			return fiber.NewError(fiber.StatusInternalServerError, err.Error())
		}
		return c.JSON(rows)
	})
	api.Post("/collections", func(c *fiber.Ctx) error {
		var body store.Collection
		if err := c.BodyParser(&body); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, err.Error())
		}
		body.Address = wallet.NormalizeAddress(body.Address)
		if body.Address == "" || !strings.HasPrefix(body.Address, "0x") || len(body.Address) != 42 {
			return fiber.NewError(fiber.StatusBadRequest, "valid 0x address required")
		}
		body.Active = true
		if err := st.UpsertCollection(c.Context(), body); err != nil {
			return fiber.NewError(fiber.StatusInternalServerError, err.Error())
		}
		return c.Status(fiber.StatusCreated).JSON(body)
	})
	api.Delete("/collections/:address", func(c *fiber.Ctx) error {
		if err := st.DeleteCollection(c.Context(), c.Params("address")); err != nil {
			return fiber.NewError(fiber.StatusNotFound, err.Error())
		}
		return c.SendStatus(fiber.StatusNoContent)
	})
	api.Post("/collections/seed", func(c *fiber.Ctx) error {
		path := "configs/collections.yaml"
		if cfg.RootDir != "" {
			path = filepath.Join(cfg.RootDir, path)
		}
		n, err := st.LoadCollectionsFile(c.Context(), path)
		if err != nil {
			return fiber.NewError(fiber.StatusInternalServerError, err.Error())
		}
		return c.JSON(fiber.Map{"loaded": n})
	})

	rpcCtx, rpcCancel := context.WithCancel(ctx)
	defer rpcCancel()
	client, err := chain.Dial(rpcCtx, cfg.RHHTTPURL, cfg.RHWSURL, cfg.ChainID)
	if err != nil {
		log.Warn("rpc unavailable for mint API", "err", err)
	}
	var engine *execute.Engine
	var memeBuyer *meme.Buyer
	if client != nil {
		defer client.Close()
		tg := alert.NewTelegram(cfg.TelegramBotToken, cfg.TelegramChatID)
		engine, err = execute.New(client.HTTP, st, log, tg, cfg.ExecutorPrivateKey, cfg.ChainID)
		if err != nil {
			log.Error("executor", "err", err)
			os.Exit(1)
		}
		go func() {
			_ = engine.Run(ctx)
		}()
		memeTg := alert.NewTelegram(cfg.TelegramBotToken, cfg.TelegramMemeChatID)
		memeBuyer, err = meme.NewBuyer(client.HTTP, st, log, memeTg, cfg.ExecutorPrivateKey, cfg.ChainID)
		if err != nil {
			log.Error("meme buyer", "err", err)
			os.Exit(1)
		}
		go func() {
			_ = memeBuyer.Run(ctx)
		}()
	}

	api.Get("/settings", func(c *fiber.Ctx) error {
		if engine == nil {
			s, err := st.GetSettings(c.Context())
			if err != nil {
				return fiber.NewError(fiber.StatusInternalServerError, err.Error())
			}
			return c.JSON(s)
		}
		view, err := engine.SettingsView(c.Context())
		if err != nil {
			return fiber.NewError(fiber.StatusInternalServerError, err.Error())
		}
		return c.JSON(view)
	})
	api.Put("/settings", func(c *fiber.Ctx) error {
		var body struct {
			MaxSpendETH     *string `json:"max_spend_eth"`
			MaxSpendWei     *string `json:"max_spend_wei"`
			ExecuteLive     *bool   `json:"execute_live"`
			AutoCopyMint    *bool   `json:"auto_copy_mint"`
			MintQuantity    *uint64 `json:"mint_quantity"`
			MemeMaxSpendETH *string `json:"meme_max_spend_eth"`
			MemeMaxSpendWei *string `json:"meme_max_spend_wei"`
			MemeExecuteLive *bool   `json:"meme_execute_live"`
			MemeAutoBuy     *bool   `json:"meme_auto_buy"`
			MemeSlippageBps *int    `json:"meme_slippage_bps"`
		}
		if err := c.BodyParser(&body); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, err.Error())
		}
		cur, err := st.GetSettings(c.Context())
		if err != nil {
			return fiber.NewError(fiber.StatusInternalServerError, err.Error())
		}
		if body.MaxSpendWei != nil && strings.TrimSpace(*body.MaxSpendWei) != "" {
			cur.MaxSpendWei = strings.TrimSpace(*body.MaxSpendWei)
		}
		if body.MaxSpendETH != nil && strings.TrimSpace(*body.MaxSpendETH) != "" {
			wei, err := ethToWei(strings.TrimSpace(*body.MaxSpendETH))
			if err != nil {
				return fiber.NewError(fiber.StatusBadRequest, err.Error())
			}
			cur.MaxSpendWei = wei
		}
		if body.ExecuteLive != nil {
			cur.ExecuteLive = *body.ExecuteLive
		}
		if body.AutoCopyMint != nil {
			cur.AutoCopyMint = *body.AutoCopyMint
		}
		if body.MintQuantity != nil {
			cur.MintQuantity = *body.MintQuantity
		}
		if body.MemeMaxSpendWei != nil && strings.TrimSpace(*body.MemeMaxSpendWei) != "" {
			cur.MemeMaxSpendWei = strings.TrimSpace(*body.MemeMaxSpendWei)
		}
		if body.MemeMaxSpendETH != nil && strings.TrimSpace(*body.MemeMaxSpendETH) != "" {
			wei, err := ethToWei(strings.TrimSpace(*body.MemeMaxSpendETH))
			if err != nil {
				return fiber.NewError(fiber.StatusBadRequest, err.Error())
			}
			cur.MemeMaxSpendWei = wei
		}
		if body.MemeExecuteLive != nil {
			cur.MemeExecuteLive = *body.MemeExecuteLive
		}
		if body.MemeAutoBuy != nil {
			cur.MemeAutoBuy = *body.MemeAutoBuy
		}
		if body.MemeSlippageBps != nil {
			cur.MemeSlippageBps = *body.MemeSlippageBps
		}
		if err := st.UpdateSettings(c.Context(), cur); err != nil {
			return fiber.NewError(fiber.StatusInternalServerError, err.Error())
		}
		if engine != nil {
			view, err := engine.SettingsView(c.Context())
			if err == nil {
				return c.JSON(view)
			}
		}
		return c.JSON(cur)
	})

	api.Get("/orders", func(c *fiber.Ctx) error {
		rows, err := st.ListOrders(c.Context(), c.QueryInt("limit", 50))
		if err != nil {
			return fiber.NewError(fiber.StatusInternalServerError, err.Error())
		}
		return c.JSON(rows)
	})
	api.Post("/mint", func(c *fiber.Ctx) error {
		if engine == nil {
			return fiber.NewError(fiber.StatusServiceUnavailable, "rpc/executor unavailable")
		}
		var body struct {
			Collection string `json:"collection"`
			Quantity   uint64 `json:"quantity"`
		}
		if err := c.BodyParser(&body); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, err.Error())
		}
		addr := wallet.NormalizeAddress(body.Collection)
		if addr == "" || !strings.HasPrefix(addr, "0x") || len(addr) != 42 {
			return fiber.NewError(fiber.StatusBadRequest, "valid collection address required")
		}
		if body.Quantity == 0 {
			body.Quantity = 1
		}
		engine.Enqueue(execute.Job{
			Source:     "manual",
			Collection: common.HexToAddress(addr),
			Quantity:   body.Quantity,
			Label:      "manual",
		})
		return c.JSON(fiber.Map{"queued": true, "collection": addr, "quantity": body.Quantity})
	})

	api.Get("/memes", func(c *fiber.Ctx) error {
		rows, err := st.ListMemeTokens(c.Context(), 30*24*time.Hour, c.QueryInt("limit", 100))
		if err != nil {
			return fiber.NewError(fiber.StatusInternalServerError, err.Error())
		}
		return c.JSON(rows)
	})
	api.Get("/memes/stats", func(c *fiber.Ctx) error {
		total, locked, alerted, err := st.MemeStats(c.Context())
		if err != nil {
			return fiber.NewError(fiber.StatusInternalServerError, err.Error())
		}
		return c.JSON(fiber.Map{"total": total, "lp_locked": locked, "alerted": alerted})
	})
	api.Get("/memes/orders", func(c *fiber.Ctx) error {
		rows, err := st.ListMemeOrders(c.Context(), c.QueryInt("limit", 50))
		if err != nil {
			return fiber.NewError(fiber.StatusInternalServerError, err.Error())
		}
		return c.JSON(rows)
	})
	api.Post("/memes/buy", func(c *fiber.Ctx) error {
		if memeBuyer == nil {
			return fiber.NewError(fiber.StatusServiceUnavailable, "meme buyer unavailable")
		}
		var body struct {
			Token string `json:"token"`
		}
		if err := c.BodyParser(&body); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, err.Error())
		}
		addr := wallet.NormalizeAddress(body.Token)
		if addr == "" || !strings.HasPrefix(addr, "0x") || len(addr) != 42 {
			return fiber.NewError(fiber.StatusBadRequest, "valid token address required")
		}
		memeBuyer.Enqueue(meme.BuyJob{
			Source: "manual",
			Token:  common.HexToAddress(addr),
			Label:  "manual",
		})
		return c.JSON(fiber.Map{"queued": true, "token": addr})
	})

	webDir := resolveWebDir()
	if webDir != "" {
		app.Static("/", webDir, fiber.Static{
			Index: "index.html",
		})
		app.Get("/*", func(c *fiber.Ctx) error {
			if strings.HasPrefix(c.Path(), "/api") {
				return fiber.ErrNotFound
			}
			return c.SendFile(filepath.Join(webDir, "index.html"))
		})
	}

	go func() {
		<-ctx.Done()
		_ = app.Shutdown()
	}()

	log.Info("api listening", "addr", cfg.HTTPAddr, "web", webDir)
	if err := app.Listen(cfg.HTTPAddr); err != nil {
		log.Error("api stopped", "err", err)
		os.Exit(1)
	}
}

func ethToWei(eth string) (string, error) {
	f, _, err := big.ParseFloat(eth, 10, 256, big.ToNearestEven)
	if err != nil {
		return "", fmt.Errorf("invalid eth amount")
	}
	if f.Sign() < 0 {
		return "", fmt.Errorf("max spend must be >= 0")
	}
	scale := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))
	weiF := new(big.Float).Mul(f, scale)
	wei, _ := weiF.Int(nil)
	return wei.String(), nil
}

func apiError(c *fiber.Ctx, err error) error {
	code := fiber.StatusInternalServerError
	if e, ok := err.(*fiber.Error); ok {
		code = e.Code
	}
	return c.Status(code).JSON(fiber.Map{"error": err.Error()})
}

func resolveWebDir() string {
	candidates := []string{"web/dist", "./web/dist", "/app/web/dist"}
	for _, p := range candidates {
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			return p
		}
	}
	return ""
}
