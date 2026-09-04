package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	HTTPAddr           string
	RHHTTPURL          string
	RHWSURL            string
	ChainID            int64
	DatabaseURL        string
	RedisURL           string
	TelegramBotToken   string
	TelegramChatID     string
	AlertOnSell        bool
	WalletsSeedPath    string
	DiscoveryInterval  time.Duration
	DiscoveryMinScore  float64
	DiscoveryMinTrades int
	DiscoveryTopN      int
	DiscoveryWindow    time.Duration
	MarketplaceEnabled bool
	LogPollInterval    time.Duration
	StartBlockLag      uint64
	RootDir            string
	ExecutorPrivateKey string
}

func Load() (Config, error) {
	root := findRoot()
	loadDotEnv(root)

	seedPath := getenv("WALLETS_SEED_PATH", "configs/wallets.seed.yaml")
	if !filepath.IsAbs(seedPath) && root != "" {
		seedPath = filepath.Join(root, seedPath)
	}

	cfg := Config{
		HTTPAddr:           getenv("HTTP_ADDR", ":8080"),
		RHHTTPURL:          getenv("RH_RPC_HTTP", "https://rpc.mainnet.chain.robinhood.com"),
		RHWSURL:            getenv("RH_RPC_WS", ""),
		ChainID:            getenvInt64("CHAIN_ID", 4663),
		DatabaseURL:        getenv("DATABASE_URL", "postgres://bot33:bot33@localhost:5433/bot33?sslmode=disable"),
		RedisURL:           getenv("REDIS_URL", "redis://localhost:6379/0"),
		TelegramBotToken:   os.Getenv("TELEGRAM_BOT_TOKEN"),
		TelegramChatID:     os.Getenv("TELEGRAM_CHAT_ID"),
		AlertOnSell:        getenvBool("ALERT_ON_SELL", false),
		WalletsSeedPath:    seedPath,
		DiscoveryInterval:  getenvDuration("DISCOVERY_INTERVAL", 1*time.Hour),
		DiscoveryMinScore:  getenvFloat("DISCOVERY_MIN_SCORE", 70),
		DiscoveryMinTrades: int(getenvInt64("DISCOVERY_MIN_TRADES", 5)),
		DiscoveryTopN:      int(getenvInt64("DISCOVERY_TOP_N", 40)),
		DiscoveryWindow:    getenvDuration("DISCOVERY_WINDOW", 30*24*time.Hour),
		MarketplaceEnabled: getenvBool("MARKETPLACE_ENABLED", true),
		LogPollInterval:    getenvDuration("LOG_POLL_INTERVAL", 3*time.Second),
		StartBlockLag:      uint64(getenvInt64("START_BLOCK_LAG", 32)),
		RootDir:            root,
		ExecutorPrivateKey: os.Getenv("EXECUTOR_PRIVATE_KEY"),
	}
	if cfg.RHHTTPURL == "" {
		return Config{}, fmt.Errorf("RH_RPC_HTTP is required")
	}
	return cfg, nil
}

func findRoot() string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return wd
		}
		dir = parent
	}
}

func loadDotEnv(root string) {
	candidates := []string{".env", ".env.local"}
	if root != "" {
		candidates = append([]string{
			filepath.Join(root, ".env"),
			filepath.Join(root, ".env.local"),
		}, candidates...)
	}
	for _, p := range candidates {
		_ = godotenv.Load(p)
	}
}

func getenv(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return def
}

func getenvBool(k string, def bool) bool {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

func getenvInt64(k string, def int64) int64 {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return def
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return def
	}
	return n
}

func getenvFloat(k string, def float64) float64 {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return def
	}
	n, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}
	return n
}

func getenvDuration(k string, def time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}
