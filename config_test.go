package loom

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadServerConfigDefaults(t *testing.T) {
	cfg := LoadServerConfig()
	want := defaultServerConfig()
	if cfg.server() != want.server() {
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
	want := AppConfig{
		Addr:              ":9090",
		Env:               "prod",
		ReadTimeout:       time.Second,
		ReadHeaderTimeout: 2 * time.Second,
		WriteTimeout:      3 * time.Second,
		IdleTimeout:       4 * time.Second,
		ShutdownTimeout:   5 * time.Second,
	}
	if cfg.server() != want.server() {
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

func TestLoadDotEnvSetsUnsetVars(t *testing.T) {
	envPath := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envPath, []byte("LOOM_TEST_DOTENV_UNSET=from-dotenv\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Unsetenv("LOOM_TEST_DOTENV_UNSET") })

	if err := LoadDotEnv(envPath); err != nil {
		t.Fatalf("LoadDotEnv: %v", err)
	}
	if got := os.Getenv("LOOM_TEST_DOTENV_UNSET"); got != "from-dotenv" {
		t.Fatalf("LOOM_TEST_DOTENV_UNSET = %q, want %q", got, "from-dotenv")
	}
}

func TestLoadDotEnvDoesNotOverrideAlreadySetVars(t *testing.T) {
	envPath := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envPath, []byte("LOOM_TEST_DOTENV_SET=from-dotenv\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// An exported-but-empty real env var must still win -- presence, not
	// truthiness, decides.
	t.Setenv("LOOM_TEST_DOTENV_SET", "")

	if err := LoadDotEnv(envPath); err != nil {
		t.Fatalf("LoadDotEnv: %v", err)
	}
	if got := os.Getenv("LOOM_TEST_DOTENV_SET"); got != "" {
		t.Fatalf("expected the already-set empty value to win, got %q", got)
	}
}

func TestLoadDotEnvMissingFileIsNotAnError(t *testing.T) {
	if err := LoadDotEnv(filepath.Join(t.TempDir(), "nonexistent", ".env")); err != nil {
		t.Fatalf("expected no error for a missing .env, got %v", err)
	}
}

func TestLoadDotEnvOtherOpenErrorIsReturned(t *testing.T) {
	// A path with a non-directory component (a file where a directory is
	// expected) fails with ENOTDIR, not ENOENT -- os.IsNotExist reports
	// false for it, so it must be returned rather than silently swallowed.
	notADir := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(notADir, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := LoadDotEnv(filepath.Join(notADir, ".env"))
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if os.IsNotExist(err) {
		t.Fatalf("expected a non-ENOENT error, got %v", err)
	}
}

func TestLoadDotEnvStripsQuotesAndSkipsCommentsAndBlankLines(t *testing.T) {
	envPath := filepath.Join(t.TempDir(), ".env")
	content := "# a comment\n\nLOOM_TEST_DOTENV_QUOTED=\"quoted value\"\nmalformed line with no equals\n"
	if err := os.WriteFile(envPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Unsetenv("LOOM_TEST_DOTENV_QUOTED") })

	if err := LoadDotEnv(envPath); err != nil {
		t.Fatalf("LoadDotEnv: %v", err)
	}
	if got := os.Getenv("LOOM_TEST_DOTENV_QUOTED"); got != "quoted value" {
		t.Fatalf("LOOM_TEST_DOTENV_QUOTED = %q, want %q", got, "quoted value")
	}
}
