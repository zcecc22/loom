package gwt

import (
	"fmt"
	"os"
	"time"
)

// ServerConfig holds runtime configuration for an App's HTTP server.
type ServerConfig struct {
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

// defaultServerConfig returns the ServerConfig used when nothing overrides it.
func defaultServerConfig() ServerConfig {
	return ServerConfig{
		Addr:              defaultAddr,
		Env:               defaultEnv,
		ReadTimeout:       defaultReadTimeout,
		ReadHeaderTimeout: defaultReadHeaderTimeout,
		WriteTimeout:      defaultWriteTimeout,
		IdleTimeout:       defaultIdleTimeout,
		ShutdownTimeout:   defaultShutdownTimeout,
	}
}

// resolveServerConfig returns base with every zero-value field in c
// replacing the corresponding field in base. New calls this with base set
// to LoadServerConfig()'s result, so the effective precedence is: an
// explicit AppConfig.Server field, then whatever the environment provided,
// then (only if neither set it) the hardcoded default LoadServerConfig
// itself would already have fallen back to. A zero timeout in c never
// means "no timeout" in http.Server — it means "unset, keep base's value"
// — so there's no way to explicitly request a zero timeout through
// AppConfig. That trade-off is deliberate; it's not something to work
// around.
func resolveServerConfig(c, base ServerConfig) ServerConfig {
	d := base
	if c.Addr != "" {
		d.Addr = c.Addr
	}
	if c.Env != "" {
		d.Env = c.Env
	}
	if c.ReadTimeout != 0 {
		d.ReadTimeout = c.ReadTimeout
	}
	if c.ReadHeaderTimeout != 0 {
		d.ReadHeaderTimeout = c.ReadHeaderTimeout
	}
	if c.WriteTimeout != 0 {
		d.WriteTimeout = c.WriteTimeout
	}
	if c.IdleTimeout != 0 {
		d.IdleTimeout = c.IdleTimeout
	}
	if c.ShutdownTimeout != 0 {
		d.ShutdownTimeout = c.ShutdownTimeout
	}
	return d
}

// LoadServerConfig reads configuration from the environment: ADDR (default
// ":8000"), ENV (default "dev"), READ_TIMEOUT (default 5s),
// READ_HEADER_TIMEOUT (default 2s), WRITE_TIMEOUT (default 10s),
// IDLE_TIMEOUT (default 120s), and SHUTDOWN_TIMEOUT (default 10s). A
// malformed *_TIMEOUT value fails fast, naming the offending variable.
func LoadServerConfig() (ServerConfig, error) {
	cfg := defaultServerConfig()

	if v := os.Getenv("ADDR"); v != "" {
		cfg.Addr = v
	}
	if v := os.Getenv("ENV"); v != "" {
		cfg.Env = v
	}

	var err error
	if cfg.ReadTimeout, err = durationEnv("READ_TIMEOUT", cfg.ReadTimeout); err != nil {
		return ServerConfig{}, err
	}
	if cfg.ReadHeaderTimeout, err = durationEnv("READ_HEADER_TIMEOUT", cfg.ReadHeaderTimeout); err != nil {
		return ServerConfig{}, err
	}
	if cfg.WriteTimeout, err = durationEnv("WRITE_TIMEOUT", cfg.WriteTimeout); err != nil {
		return ServerConfig{}, err
	}
	if cfg.IdleTimeout, err = durationEnv("IDLE_TIMEOUT", cfg.IdleTimeout); err != nil {
		return ServerConfig{}, err
	}
	if cfg.ShutdownTimeout, err = durationEnv("SHUTDOWN_TIMEOUT", cfg.ShutdownTimeout); err != nil {
		return ServerConfig{}, err
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
