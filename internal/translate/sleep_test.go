package translate

import (
	"context"
	"testing"
	"time"
)

func TestDefaultSleep_ReturnsNilAfterDuration(t *testing.T) {
	t.Parallel()

	start := time.Now()
	if err := defaultSleep(context.Background(), 10*time.Millisecond); err != nil {
		t.Fatalf("defaultSleep() error = %v", err)
	}
	if elapsed := time.Since(start); elapsed < 10*time.Millisecond {
		t.Errorf("defaultSleep() retornou após %v, want >= 10ms", elapsed)
	}
}

func TestDefaultSleep_AbortsOnContextCancel(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := defaultSleep(ctx, time.Second); err == nil {
		t.Error("defaultSleep() error = nil, want context error")
	}
}
