package srv

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config holds all application configuration, loadable from .env or environment variables.
type Config struct {
	// Server
	ListenAddr string // LISTEN_ADDR - address to listen on (default ":8000")
	DBPath     string // DB_PATH - path to SQLite database (default "db.sqlite3")
	LogFile    string // LOG_FILE - path to log file (default "logs/newsfornerds.log")
	LogLevel       string // LOG_LEVEL - log level: debug, info, warn, error (default "info")
	CanonicalDomain string // CANONICAL_DOMAIN - if set, redirects requests from other hosts to this domain

	// Google OAuth
	GoogleClientID     string // GOOGLE_CLIENT_ID
	GoogleClientSecret string // GOOGLE_CLIENT_SECRET

	// Feed settings
	FeedRefreshInterval int // FEED_REFRESH_INTERVAL - minutes between feed refresh cycles (default 1)
	FeedStaleMinutes    int // FEED_STALE_MINUTES - minutes before a feed is considered stale (default 5)
	FeedErrorBackoff    int // FEED_ERROR_BACKOFF - minutes before retrying errored feeds (default 30)
	FeedMaxItems        int // FEED_MAX_ITEMS - max items to store per feed (default 50)
	FeedMaxPerCycle     int // FEED_MAX_PER_CYCLE - max feeds to refresh per cycle (default 5)
	FeedFetchTimeout    int // FEED_FETCH_TIMEOUT - seconds for feed HTTP requests (default 30)

	// Session & cookies
	SessionDurationDays int // SESSION_DURATION_DAYS - session cookie lifetime (default 30)
	VisitorDurationDays int // VISITOR_DURATION_DAYS - visitor cookie lifetime (default 365)

	// Defaults for new pages/widgets
	DefaultBgColor      string // DEFAULT_BG_COLOR - default page background (default "#1a1a2e")
	DefaultWidgetBg     string // DEFAULT_WIDGET_BG - default widget background (default "#16213e")
	DefaultWidgetText   string // DEFAULT_WIDGET_TEXT - default widget text color (default "#ffffff")
	DefaultWidgetHeader string // DEFAULT_WIDGET_HEADER - default widget header color (default "#0f3460")

	// Visited link cleanup
	VisitedLinkMaxDays int // VISITED_LINK_MAX_DAYS - days to keep visited link records (default 30)
}

// DefaultConfig returns a Config with all defaults set.
func DefaultConfig() *Config {
	return &Config{
		ListenAddr:          ":8000",
		DBPath:              "db.sqlite3",
		LogFile:             "logs/newsfornerds.log",
		LogLevel:            "info",
		FeedRefreshInterval: 1,
		FeedStaleMinutes:    5,
		FeedErrorBackoff:    30,
		FeedMaxItems:        50,
		FeedMaxPerCycle:     5,
		FeedFetchTimeout:    30,
		SessionDurationDays: 30,
		VisitorDurationDays: 365,
		DefaultBgColor:      "#1a1a2e",
		DefaultWidgetBg:     "#16213e",
		DefaultWidgetText:   "#ffffff",
		DefaultWidgetHeader: "#0f3460",
		VisitedLinkMaxDays:  30,
	}
}

// LoadConfig loads configuration from .env file (if present) and environment variables.
// Environment variables override .env file values.
func LoadConfig(envFile string) (*Config, error) {
	cfg := DefaultConfig()

	// Load .env file if it exists (don't error if missing)
	if envFile != "" {
		if err := loadEnvFile(envFile); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("load .env: %w", err)
		}
	}

	// Read all config from environment (which now includes .env values)
	cfg.ListenAddr = envOrDefault("LISTEN_ADDR", cfg.ListenAddr)
	cfg.DBPath = envOrDefault("DB_PATH", cfg.DBPath)
	cfg.LogFile = envOrDefault("LOG_FILE", cfg.LogFile)
	cfg.LogLevel = envOrDefault("LOG_LEVEL", cfg.LogLevel)
	cfg.CanonicalDomain = envOrDefault("CANONICAL_DOMAIN", cfg.CanonicalDomain)

	cfg.GoogleClientID = envOrDefault("GOOGLE_CLIENT_ID", cfg.GoogleClientID)
	cfg.GoogleClientSecret = envOrDefault("GOOGLE_CLIENT_SECRET", cfg.GoogleClientSecret)

	cfg.FeedRefreshInterval = envOrDefaultInt("FEED_REFRESH_INTERVAL", cfg.FeedRefreshInterval)
	cfg.FeedStaleMinutes = envOrDefaultInt("FEED_STALE_MINUTES", cfg.FeedStaleMinutes)
	cfg.FeedErrorBackoff = envOrDefaultInt("FEED_ERROR_BACKOFF", cfg.FeedErrorBackoff)
	cfg.FeedMaxItems = envOrDefaultInt("FEED_MAX_ITEMS", cfg.FeedMaxItems)
	cfg.FeedMaxPerCycle = envOrDefaultInt("FEED_MAX_PER_CYCLE", cfg.FeedMaxPerCycle)
	cfg.FeedFetchTimeout = envOrDefaultInt("FEED_FETCH_TIMEOUT", cfg.FeedFetchTimeout)

	cfg.SessionDurationDays = envOrDefaultInt("SESSION_DURATION_DAYS", cfg.SessionDurationDays)
	cfg.VisitorDurationDays = envOrDefaultInt("VISITOR_DURATION_DAYS", cfg.VisitorDurationDays)

	cfg.DefaultBgColor = envOrDefault("DEFAULT_BG_COLOR", cfg.DefaultBgColor)
	cfg.DefaultWidgetBg = envOrDefault("DEFAULT_WIDGET_BG", cfg.DefaultWidgetBg)
	cfg.DefaultWidgetText = envOrDefault("DEFAULT_WIDGET_TEXT", cfg.DefaultWidgetText)
	cfg.DefaultWidgetHeader = envOrDefault("DEFAULT_WIDGET_HEADER", cfg.DefaultWidgetHeader)

	cfg.VisitedLinkMaxDays = envOrDefaultInt("VISITED_LINK_MAX_DAYS", cfg.VisitedLinkMaxDays)

	return cfg, nil
}

// loadEnvFile parses a .env file and sets environment variables.
// Does NOT override variables already set in the environment.
func loadEnvFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// Skip comments and blank lines
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Split on first =
		idx := strings.IndexByte(line, '=')
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		// Remove surrounding quotes
		if len(val) >= 2 && ((val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'')) {
			val = val[1 : len(val)-1]
		}
		// Only set if not already in environment (env vars take precedence)
		if _, exists := os.LookupEnv(key); !exists {
			os.Setenv(key, val)
		}
	}
	return scanner.Err()
}

func envOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func envOrDefaultInt(key string, defaultVal int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return defaultVal
}
