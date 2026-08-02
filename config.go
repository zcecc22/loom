package gwt

import (
	"fmt"
	"os"
	"time"
)

// Config holds runtime configuration for an App's HTTP server.
type Config struct {
	// Addr is the address the server listens on, e.g. ":8000".
	Addr string
	// Env is the environment name (e.g. "dev", "prod"); it selects
	// NewLogger's default handler format.
	Env string

	// ReadTimeout is http.Server.ReadTimeout.
	ReadTimeout time.Duration
	// ReadHeaderTimeout is http.Server.ReadHeaderTimeout.
	ReadHeaderTimeout time.Duration
	// WriteTimeout is http.Server.WriteTimeout.
	WriteTimeout time.Duration
	// IdleTimeout is http.Server.IdleTimeout.
	IdleTimeout time.Duration
	// ShutdownTimeout is the grace period RunContext gives in-flight
	// requests to complete during a graceful shutdown.
	ShutdownTimeout time.Duration
}

const (
	defaultAddr              = ":8000"
	defaultEnv               = "dev"
	defaultReadTimeout       = 5 * time.Second
	defaultReadHeaderTimeout = 2 * time.Second
	defaultWriteTimeout      = 10 * time.Second
	defaultIdleTimeout       = 120 * time.Second
	defaultShutdownTimeout   = 10 * time.Second
)

// defaultConfig returns the Config used when no options and no environment
// variables override it.
func defaultConfig() Config {
	return Config{
		Addr:              defaultAddr,
		Env:               defaultEnv,
		ReadTimeout:       defaultReadTimeout,
		ReadHeaderTimeout: defaultReadHeaderTimeout,
		WriteTimeout:      defaultWriteTimeout,
		IdleTimeout:       defaultIdleTimeout,
		ShutdownTimeout:   defaultShutdownTimeout,
	}
}

// LoadConfig reads configuration from the environment: ADDR (default
// ":8000"), ENV (default "dev"), READ_TIMEOUT (default 5s),
// READ_HEADER_TIMEOUT (default 2s), WRITE_TIMEOUT (default 10s),
// IDLE_TIMEOUT (default 120s), and SHUTDOWN_TIMEOUT (default 10s). A
// malformed *_TIMEOUT value fails fast, naming the offending variable.
func LoadConfig() (Config, error) {
	cfg := defaultConfig()

	if v := os.Getenv("ADDR"); v != "" {
		cfg.Addr = v
	}
	if v := os.Getenv("ENV"); v != "" {
		cfg.Env = v
	}

	var err error
	if cfg.ReadTimeout, err = durationEnv("READ_TIMEOUT", cfg.ReadTimeout); err != nil {
		return Config{}, err
	}
	if cfg.ReadHeaderTimeout, err = durationEnv("READ_HEADER_TIMEOUT", cfg.ReadHeaderTimeout); err != nil {
		return Config{}, err
	}
	if cfg.WriteTimeout, err = durationEnv("WRITE_TIMEOUT", cfg.WriteTimeout); err != nil {
		return Config{}, err
	}
	if cfg.IdleTimeout, err = durationEnv("IDLE_TIMEOUT", cfg.IdleTimeout); err != nil {
		return Config{}, err
	}
	if cfg.ShutdownTimeout, err = durationEnv("SHUTDOWN_TIMEOUT", cfg.ShutdownTimeout); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

// durationEnv reads key from the environment as a time.Duration, returning
// fallback if key is unset. A set-but-unparsable value is a fail-fast error
// naming key.
func durationEnv(key string, fallback time.Duration) (time.Duration, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("gwt: invalid %s: %w", key, err)
	}
	return d, nil
}
