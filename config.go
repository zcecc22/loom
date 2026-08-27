package loom

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"
)

// serverConfig is the comparable subset of AppConfig's server-relevant
// fields that App stores internally after New resolves them. AppConfig
// itself is not comparable (it holds slice/func fields for OnStart,
// OnShutdown, ErrorHandler, and BaseContext), so App keeps only this
// narrower type rather than a full AppConfig.
type serverConfig struct {
	Addr, Env         string
	ReadTimeout       time.Duration
	ReadHeaderTimeout time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration
}

// server extracts c's 7 server-relevant fields into a serverConfig.
func (c AppConfig) server() serverConfig {
	return serverConfig{
		Addr: c.Addr, Env: c.Env,
		ReadTimeout: c.ReadTimeout, ReadHeaderTimeout: c.ReadHeaderTimeout,
		WriteTimeout: c.WriteTimeout, IdleTimeout: c.IdleTimeout,
		ShutdownTimeout: c.ShutdownTimeout,
	}
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

// defaultServerConfig returns the AppConfig (only its 7 server-relevant
// fields populated) used when nothing overrides it.
func defaultServerConfig() AppConfig {
	return AppConfig{
		Addr:              defaultAddr,
		Env:               defaultEnv,
		ReadTimeout:       defaultReadTimeout,
		ReadHeaderTimeout: defaultReadHeaderTimeout,
		WriteTimeout:      defaultWriteTimeout,
		IdleTimeout:       defaultIdleTimeout,
		ShutdownTimeout:   defaultShutdownTimeout,
	}
}

// resolveServerConfig returns c with every zero-value server field in c
// replaced by the corresponding field in base; c's non-server fields
// (Logger, Templates, ErrorHandler, ...) pass through unchanged, since base
// (LoadServerConfig()'s result) never populates them. New calls this with
// base set to LoadServerConfig()'s result, so the effective precedence is:
// an explicit AppConfig field, then whatever the environment provided, then
// (only if neither set it) the hardcoded default LoadServerConfig itself
// would already have fallen back to. A zero timeout in c never means "no
// timeout" in http.Server — it means "unset, keep base's value" — so
// there's no way to explicitly request a zero timeout through AppConfig.
// That trade-off is deliberate; it's not something to work around.
func resolveServerConfig(c, base AppConfig) AppConfig {
	d := c
	if d.Addr == "" {
		d.Addr = base.Addr
	}
	if d.Env == "" {
		d.Env = base.Env
	}
	if d.ReadTimeout == 0 {
		d.ReadTimeout = base.ReadTimeout
	}
	if d.ReadHeaderTimeout == 0 {
		d.ReadHeaderTimeout = base.ReadHeaderTimeout
	}
	if d.WriteTimeout == 0 {
		d.WriteTimeout = base.WriteTimeout
	}
	if d.IdleTimeout == 0 {
		d.IdleTimeout = base.IdleTimeout
	}
	if d.ShutdownTimeout == 0 {
		d.ShutdownTimeout = base.ShutdownTimeout
	}
	return d
}

// LoadServerConfig reads configuration from the environment: ADDR (default
// ":8000"), ENV (default "dev"), READ_TIMEOUT (default 5s),
// READ_HEADER_TIMEOUT (default 2s), WRITE_TIMEOUT (default 10s),
// IDLE_TIMEOUT (default 120s), and SHUTDOWN_TIMEOUT (default 10s), into an
// AppConfig with only those 7 fields populated. A malformed *_TIMEOUT value
// panics, naming the offending variable — a construction-time
// misconfiguration, not a per-request condition, so it fails loudly at
// setup rather than being swallowed into a runtime error. The returned
// AppConfig can be passed straight to New, or have further fields (Logger,
// Templates, ...) set on it first.
func LoadServerConfig() AppConfig {
	cfg := defaultServerConfig()

	if v := os.Getenv("ADDR"); v != "" {
		cfg.Addr = v
	}
	if v := os.Getenv("ENV"); v != "" {
		cfg.Env = v
	}

	cfg.ReadTimeout = durationEnv("READ_TIMEOUT", cfg.ReadTimeout)
	cfg.ReadHeaderTimeout = durationEnv("READ_HEADER_TIMEOUT", cfg.ReadHeaderTimeout)
	cfg.WriteTimeout = durationEnv("WRITE_TIMEOUT", cfg.WriteTimeout)
	cfg.IdleTimeout = durationEnv("IDLE_TIMEOUT", cfg.IdleTimeout)
	cfg.ShutdownTimeout = durationEnv("SHUTDOWN_TIMEOUT", cfg.ShutdownTimeout)

	return cfg
}

// LoadDotEnv reads KEY=VALUE lines from path into the process environment,
// skipping blank/comment lines and never overriding a key that's already
// set — presence, not truthiness, decides, so a real deployment's or
// Docker's env_file:-injected variables always win over a checked-out .env
// file. Call it before New or LoadServerConfig, since both read the
// environment themselves. A missing file is not an error; any other
// Open/Setenv/scan error is returned. A value wrapped in matching quotes
// has them stripped.
func LoadDotEnv(path string) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		val := strings.TrimSpace(line[eq+1:])
		if len(val) >= 2 {
			first, last := val[0], val[len(val)-1]
			if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
				val = val[1 : len(val)-1]
			}
		}
		if _, present := os.LookupEnv(key); present {
			continue
		}
		if err := os.Setenv(key, val); err != nil {
			return err
		}
	}
	return scanner.Err()
}

// durationEnv reads key from the environment as a time.Duration, returning
// fallback if key is unset. A set-but-unparsable value panics, naming key.
func durationEnv(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		panic(fmt.Sprintf("loom: invalid %s: %v", key, err))
	}
	return d
}
