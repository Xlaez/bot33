package config

import (
	"fmt"
	"math/big"
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
	TelegramMemeChatID string
	AlertOnSell        bool
	WalletsSeedPath    string
	DiscoveryInterval  time.Duration
	DiscoveryMinScore  float64
	DiscoveryMinTrades int
	DiscoveryTopN      int
	DiscoveryWindow    time.Duration
	MarketplaceEnabled      bool
	MarketplacePollInterval time.Duration
	AlertSeaportMinWei      string
	AlertNotifyMinScore     float64
	AlertHeatWindow         time.Duration
	AlertHeatMinSales       int
	AlertPremiumMultiple    float64
	CollectionsPath         string
	MemePollInterval        time.Duration
	LogPollInterval         time.Duration
	StartBlockLag           uint64
	RootDir                 string
	ExecutorPrivateKey      string
	ExecutorPrivateKeys     []string
}

func Load() (Config, error) {
	root := findRoot()
	loadDotEnv(root)

	seedPath := getenv("WALLETS_SEED_PATH", "configs/wallets.seed.yaml")
	if !filepath.IsAbs(seedPath) && root != "" {
		seedPath = filepath.Join(root, seedPath)
	}
	collPath := getenv("COLLECTIONS_PATH", "configs/collections.yaml")
	if !filepath.IsAbs(collPath) && root != "" {
		collPath = filepath.Join(root, collPath)
	}

	cfg := Config{
		HTTPAddr:                getenv("HTTP_ADDR", ":8080"),
		RHHTTPURL:               getenv("RH_RPC_HTTP", "https://rpc.mainnet.chain.robinhood.com"),
		RHWSURL:                 getenv("RH_RPC_WS", ""),
		ChainID:                 getenvInt64("CHAIN_ID", 4663),
		DatabaseURL:             getenv("DATABASE_URL", "postgres://bot33:bot33@localhost:5433/bot33?sslmode=disable"),
		RedisURL:                getenv("REDIS_URL", "redis://localhost:6379/0"),
		TelegramBotToken:        os.Getenv("TELEGRAM_BOT_TOKEN"),
		TelegramChatID:          os.Getenv("TELEGRAM_CHAT_ID"),
		TelegramMemeChatID:      os.Getenv("TELEGRAM_MEME_CHAT_ID"),
		AlertOnSell:             getenvBool("ALERT_ON_SELL", false),
		WalletsSeedPath:         seedPath,
		DiscoveryInterval:       getenvDuration("DISCOVERY_INTERVAL", 5*time.Minute),
		DiscoveryMinScore:       getenvFloat("DISCOVERY_MIN_SCORE", 70),
		DiscoveryMinTrades:      int(getenvInt64("DISCOVERY_MIN_TRADES", 5)),
		DiscoveryTopN:           int(getenvInt64("DISCOVERY_TOP_N", 40)),
		DiscoveryWindow:         getenvDuration("DISCOVERY_WINDOW", 30*24*time.Hour),
		MarketplaceEnabled:      getenvBool("MARKETPLACE_ENABLED", true),
		MarketplacePollInterval: getenvDuration("MARKETPLACE_POLL_INTERVAL", 8*time.Second),
		AlertSeaportMinWei:      ethEnvToWei("ALERT_SEAPORT_MIN_ETH", "0.01"),
		AlertNotifyMinScore:     getenvFloat("ALERT_NOTIFY_MIN_SCORE", 60),
		AlertHeatWindow:         getenvDuration("ALERT_HEAT_WINDOW", 30*time.Minute),
		AlertHeatMinSales:       int(getenvInt64("ALERT_HEAT_MIN_SALES", 3)),
		AlertPremiumMultiple:    getenvFloat("ALERT_PREMIUM_MULTIPLE", 1.5),
		CollectionsPath:         collPath,
		MemePollInterval:        getenvDuration("MEME_POLL_INTERVAL", 5*time.Second),
		LogPollInterval:         getenvDuration("LOG_POLL_INTERVAL", 3*time.Second),
		StartBlockLag:           uint64(getenvInt64("START_BLOCK_LAG", 32)),
		RootDir:                 root,
		ExecutorPrivateKey:      os.Getenv("EXECUTOR_PRIVATE_KEY"),
		ExecutorPrivateKeys:     parseKeyList(os.Getenv("EXECUTOR_PRIVATE_KEYS"), os.Getenv("EXECUTOR_PRIVATE_KEY")),
	}
	if cfg.RHHTTPURL == "" {
		return Config{}, fmt.Errorf("RH_RPC_HTTP is required")
	}
	return cfg, nil
}

func parseKeyList(multi, single string) []string {
	var out []string
	seen := map[string]struct{}{}
	add := func(raw string) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return
		}
		key := strings.ToLower(raw)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, raw)
	}
	for _, part := range strings.Split(multi, ",") {
		add(part)
	}
	add(single)
	return out
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

// ethEnvToWei reads an ETH decimal env (e.g. 0.01) and returns wei as decimal string.
// Empty or "0" disables the threshold alert path (returns "0").
func ethEnvToWei(key, defEth string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		v = defEth
	}
	if v == "0" || v == "0.0" {
		return "0"
	}
	f, _, err := bigFloat().Parse(v, 10)
	if err != nil {
		f, _, err = bigFloat().Parse(defEth, 10)
		if err != nil {
			return "0"
		}
	}
	wei := new(big.Float).Mul(f, big.NewFloat(1e18))
	out, _ := wei.Int(nil)
	if out == nil {
		return "0"
	}
	return out.String()
}

func bigFloat() *big.Float {
	return new(big.Float).SetPrec(256)
}
