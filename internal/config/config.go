package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
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
	LogPollInterval    time.Duration
	StartBlockLag      uint64
}

func Load() (Config, error) {
	cfg := Config{
		HTTPAddr:           getenv("HTTP_ADDR", ":8080"),
		RHHTTPURL:          getenv("RH_RPC_HTTP", "https://rpc.mainnet.chain.robinhood.com"),
		RHWSURL:            getenv("RH_RPC_WS", ""),
		ChainID:            getenvInt64("CHAIN_ID", 4663),
		DatabaseURL:        getenv("DATABASE_URL", "postgres://bot33:bot33@localhost:5432/bot33?sslmode=disable"),
		RedisURL:           getenv("REDIS_URL", "redis://localhost:6379/0"),
		TelegramBotToken:   os.Getenv("TELEGRAM_BOT_TOKEN"),
		TelegramChatID:     os.Getenv("TELEGRAM_CHAT_ID"),
		AlertOnSell:        getenvBool("ALERT_ON_SELL", false),
		WalletsSeedPath:    getenv("WALLETS_SEED_PATH", "configs/wallets.seed.yaml"),
		DiscoveryInterval:  getenvDuration("DISCOVERY_INTERVAL", 1*time.Hour),
		DiscoveryMinScore:  getenvFloat("DISCOVERY_MIN_SCORE", 70),
		DiscoveryMinTrades: int(getenvInt64("DISCOVERY_MIN_TRADES", 5)),
		LogPollInterval:    getenvDuration("LOG_POLL_INTERVAL", 3*time.Second),
		StartBlockLag:      uint64(getenvInt64("START_BLOCK_LAG", 32)),
	}
	if cfg.RHHTTPURL == "" {
		return Config{}, fmt.Errorf("RH_RPC_HTTP is required")
	}
	return cfg, nil
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
