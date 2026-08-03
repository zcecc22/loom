package gwt

import (
	"strings"
	"testing"
	"time"
)

func TestLoadServerConfigDefaults(t *testing.T) {
	cfg, err := LoadServerConfig()
	if err != nil {
		t.Fatalf("LoadServerConfig() error = %v", err)
	}
	want := defaultServerConfig()
	if cfg != want {
		t.Fatalf("LoadServerConfig() = %+v, want %+v", cfg, want)
	}
}

func TestLoadServerConfigOverrides(t *testing.T) {
	t.Setenv("ADDR", ":9090")
	t.Setenv("ENV", "prod")
	t.Setenv("READ_TIMEOUT", "1s")
	t.Setenv("READ_HEADER_TIMEOUT", "2s")
	t.Setenv("WRITE_TIMEOUT", "3s")
	t.Setenv("IDLE_TIMEOUT", "4s")
	t.Setenv("SHUTDOWN_TIMEOUT", "5s")

	cfg, err := LoadServerConfig()
	if err != nil {
		t.Fatalf("LoadServerConfig() error = %v", err)
	}
	want := ServerConfig{
		Addr:              ":9090",
		Env:               "prod",
		ReadTimeout:       time.Second,
		ReadHeaderTimeout: 2 * time.Second,
		WriteTimeout:      3 * time.Second,
		IdleTimeout:       4 * time.Second,
		ShutdownTimeout:   5 * time.Second,
	}
	if cfg != want {
		t.Fatalf("LoadServerConfig() = %+v, want %+v", cfg, want)
	}
}

func TestLoadServerConfigMalformedDurationNamesVariable(t *testing.T) {
	t.Setenv("WRITE_TIMEOUT", "notaduration")

	_, err := LoadServerConfig()
	if err == nil {
		t.Fatal("LoadServerConfig() error = nil, want error naming WRITE_TIMEOUT")
	}
	if !strings.Contains(err.Error(), "WRITE_TIMEOUT") {
		t.Fatalf("error = %q, want it to name WRITE_TIMEOUT", err.Error())
	}
}

func TestLoadServerConfigMalformedDurationEachField(t *testing.T) {
	for _, key := range []string{"READ_TIMEOUT", "READ_HEADER_TIMEOUT", "WRITE_TIMEOUT", "IDLE_TIMEOUT", "SHUTDOWN_TIMEOUT"} {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, "nope")
			_, err := LoadServerConfig()
			if err == nil {
				t.Fatalf("LoadServerConfig() error = nil, want error naming %s", key)
			}
			if !strings.Contains(err.Error(), key) {
				t.Fatalf("error = %q, want it to name %s", err.Error(), key)
			}
		})
	}
}
