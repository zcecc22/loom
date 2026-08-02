package gwt

import (
	"fmt"
	"testing"
)

func TestNewLoggerHandlerTypes(t *testing.T) {
	tests := []struct {
		env  string
		want string
	}{
		{"dev", "*slog.TextHandler"},
		{"prod", "*slog.JSONHandler"},
		{"staging", "*slog.JSONHandler"},
		{"", "*slog.JSONHandler"},
	}

	for _, tt := range tests {
		t.Run(tt.env, func(t *testing.T) {
			logger := NewLogger(tt.env)
			if logger == nil {
				t.Fatal("NewLogger returned nil")
			}
			got := fmt.Sprintf("%T", logger.Handler())
			if got != tt.want {
				t.Fatalf("handler type for env=%q = %s, want %s", tt.env, got, tt.want)
			}
		})
	}
}
