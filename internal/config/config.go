// Package config loads SoroLens configuration from environment variables.
package config

import (
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// SourceMode selects where SoroLens reads contract events from.
type SourceMode string

const (
	// ModeRPC is standalone mode: SoroLens polls a Stellar RPC endpoint
	// itself and stores events in its own Postgres database.
	ModeRPC SourceMode = "rpc"
	// ModeSoroTrail is upstream mode: SoroLens reads from a SoroTrail
	// indexer's HTTP API and keeps no database of its own.
	ModeSoroTrail SourceMode = "sorotrail"
)

// Defaults used when the corresponding environment variable is unset.
const (
	DefaultSourceMode   = ModeRPC
	DefaultRPCURL       = "https://soroban-testnet.stellar.org"
	DefaultPollInterval = 5 * time.Second
	DefaultHTTPAddr     = ":8080"
	// DefaultRetentionLedgers is how far back a cold start reaches when
	// START_LEDGER is unset — roughly 24h at ~5s per ledger. Stellar RPC
	// retains events for about 24h to 7d, so reaching further back than the
	// node's own window simply yields nothing.
	DefaultRetentionLedgers = 17280
)

// Config holds all runtime configuration. Every field maps to one environment
// variable; see .env.example for the full list.
type Config struct {
	// SourceMode selects the EventSource implementation (rpc or sorotrail).
	SourceMode SourceMode

	// RPCURL is the Stellar RPC endpoint (JSON-RPC 2.0 over HTTP). Standalone
	// mode only.
	RPCURL string
	// DatabaseURL is a Postgres connection string in pgx format. Standalone
	// mode only; upstream mode uses no database.
	DatabaseURL string
	// PollInterval is how long the ingester sleeps between polls once it has
	// caught up to the latest ledger. Standalone mode only.
	PollInterval time.Duration
	// WatchedContracts limits ingestion to these contract IDs. Empty means
	// ingest events from every contract. Standalone mode only.
	WatchedContracts []string
	// StartLedger forces a cold start from this ledger instead of reaching
	// back RetentionLedgers from the chain tip. Standalone mode only.
	StartLedger uint32
	// RetentionLedgers is the cold-start reach-back when StartLedger is unset.
	RetentionLedgers uint32

	// SoroTrailURL is the base URL of a SoroTrail indexer's HTTP API, e.g.
	// http://localhost:8080. Upstream mode only.
	SoroTrailURL string

	// HTTPAddr is the listen address for the web UI and JSON API.
	HTTPAddr string
	// LogLevel is the minimum slog level (debug, info, warn, error).
	LogLevel slog.Level
}

// UsesDatabase reports whether the selected mode needs Postgres. Upstream mode
// reads everything from SoroTrail, so main skips migrations and the pool.
func (c Config) UsesDatabase() bool { return c.SourceMode == ModeRPC }

// Load reads configuration from the environment and validates that the
// variables required by the selected SOURCE_MODE are present.
func Load() (Config, error) {
	cfg := Config{
		SourceMode:       DefaultSourceMode,
		RPCURL:           getenv("RPC_URL", DefaultRPCURL),
		DatabaseURL:      os.Getenv("DATABASE_URL"),
		PollInterval:     DefaultPollInterval,
		SoroTrailURL:     strings.TrimRight(os.Getenv("SOROTRAIL_URL"), "/"),
		RetentionLedgers: DefaultRetentionLedgers,
		HTTPAddr:         getenv("HTTP_ADDR", DefaultHTTPAddr),
		LogLevel:         slog.LevelInfo,
	}

	if v := os.Getenv("SOURCE_MODE"); v != "" {
		switch SourceMode(strings.ToLower(v)) {
		case ModeRPC:
			cfg.SourceMode = ModeRPC
		case ModeSoroTrail:
			cfg.SourceMode = ModeSoroTrail
		default:
			return cfg, fmt.Errorf("invalid SOURCE_MODE %q (want %q or %q)", v, ModeRPC, ModeSoroTrail)
		}
	}

	if v := os.Getenv("POLL_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return cfg, fmt.Errorf("invalid POLL_INTERVAL %q: %w", v, err)
		}
		if d < time.Second {
			return cfg, fmt.Errorf("POLL_INTERVAL %q is below the 1s minimum", v)
		}
		cfg.PollInterval = d
	}

	if v := os.Getenv("WATCHED_CONTRACTS"); v != "" {
		for _, part := range strings.Split(v, ",") {
			id := strings.TrimSpace(part)
			if id == "" {
				continue
			}
			if !ValidContractID(id) {
				return cfg, fmt.Errorf("invalid contract ID %q in WATCHED_CONTRACTS", id)
			}
			cfg.WatchedContracts = append(cfg.WatchedContracts, id)
		}
	}

	if v := os.Getenv("START_LEDGER"); v != "" {
		n, err := strconv.ParseUint(v, 10, 32)
		if err != nil || n == 0 {
			return cfg, fmt.Errorf("invalid START_LEDGER %q: want a positive integer", v)
		}
		cfg.StartLedger = uint32(n)
	}

	if v := os.Getenv("RETENTION_LEDGERS"); v != "" {
		n, err := strconv.ParseUint(v, 10, 32)
		if err != nil || n == 0 {
			return cfg, fmt.Errorf("invalid RETENTION_LEDGERS %q: want a positive integer", v)
		}
		cfg.RetentionLedgers = uint32(n)
	}

	if v := os.Getenv("LOG_LEVEL"); v != "" {
		lvl, err := parseLevel(v)
		if err != nil {
			return cfg, err
		}
		cfg.LogLevel = lvl
	}

	if err := cfg.validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// validate enforces the per-mode requirements, failing fast with a message
// that names both the missing variable and the mode that requires it.
func (c Config) validate() error {
	switch c.SourceMode {
	case ModeRPC:
		if c.DatabaseURL == "" {
			return fmt.Errorf("DATABASE_URL is required when SOURCE_MODE=%s (standalone mode stores events itself)", ModeRPC)
		}
		if c.RPCURL == "" {
			return fmt.Errorf("RPC_URL is required when SOURCE_MODE=%s", ModeRPC)
		}
	case ModeSoroTrail:
		if c.SoroTrailURL == "" {
			return fmt.Errorf("SOROTRAIL_URL is required when SOURCE_MODE=%s (upstream mode reads from a SoroTrail indexer)", ModeSoroTrail)
		}
		if !strings.HasPrefix(c.SoroTrailURL, "http://") && !strings.HasPrefix(c.SoroTrailURL, "https://") {
			return fmt.Errorf("SOROTRAIL_URL %q must start with http:// or https://", c.SoroTrailURL)
		}
	default:
		return fmt.Errorf("unknown SOURCE_MODE %q", c.SourceMode)
	}

	if c.HTTPAddr == "" {
		return fmt.Errorf("HTTP_ADDR must not be empty")
	}
	return nil
}

// contractIDPattern matches a strkey-encoded contract ID: 'C' followed by 55
// base32 characters.
var contractIDPattern = regexp.MustCompile(`^C[A-Z2-7]{55}$`)

// ValidContractID reports whether s looks like a Soroban contract ID. This is
// a shape check, not a checksum verification.
func ValidContractID(s string) bool { return contractIDPattern.MatchString(s) }

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func parseLevel(s string) (slog.Level, error) {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf("invalid LOG_LEVEL %q (want debug, info, warn or error)", s)
	}
}
