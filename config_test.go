package gwt

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestLoadServerConfigDefaults(t *testing.T) {
	cfg := LoadServerConfig()
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

	cfg := LoadServerConfig()
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

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected LoadServerConfig() to panic naming WRITE_TIMEOUT")
		}
		msg := fmt.Sprintf("%v", r)
		if !strings.Contains(msg, "WRITE_TIMEOUT") {
			t.Fatalf("panic message = %q, want it to name WRITE_TIMEOUT", msg)
		}
	}()
	LoadServerConfig()
}

func TestLoadServerConfigMalformedDurationEachField(t *testing.T) {
	for _, key := range []string{"READ_TIMEOUT", "READ_HEADER_TIMEOUT", "WRITE_TIMEOUT", "IDLE_TIMEOUT", "SHUTDOWN_TIMEOUT"} {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, "nope")

			defer func() {
				r := recover()
				if r == nil {
					t.Fatalf("expected LoadServerConfig() to panic naming %s", key)
				}
				msg := fmt.Sprintf("%v", r)
				if !strings.Contains(msg, key) {
					t.Fatalf("panic message = %q, want it to name %s", msg, key)
				}
			}()
			LoadServerConfig()
		})
	}
}
