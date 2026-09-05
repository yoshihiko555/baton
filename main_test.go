package main

import (
	"errors"
	"testing"
)

func TestGuardWriteStatus(t *testing.T) {
	t.Run("writes when hook socket is available", func(t *testing.T) {
		wantErr := errors.New("write failed")
		calls := 0
		writeStatus := guardWriteStatus(func() error {
			calls++
			return wantErr
		}, false, "/tmp/status.json")

		if err := writeStatus(); !errors.Is(err, wantErr) {
			t.Fatalf("writeStatus() error = %v, want %v", err, wantErr)
		}
		if calls != 1 {
			t.Errorf("writeStatus calls = %d, want 1", calls)
		}
	})

	t.Run("does not write when another instance is listening", func(t *testing.T) {
		calls := 0
		writeStatus := guardWriteStatus(func() error {
			calls++
			return errors.New("unexpected write")
		}, true, "/tmp/status.json")

		if err := writeStatus(); err != nil {
			t.Fatalf("writeStatus() error = %v, want nil", err)
		}
		if calls != 0 {
			t.Errorf("writeStatus calls = %d, want 0", calls)
		}
	})
}

func TestEffectiveVersionPrefersEmbeddedVersionWhenSet(t *testing.T) {
	got := effectiveVersion("0.1.2-6-g3f0365f", "v0.1.3-0.20260706064427-3f0365fd6200")
	if got != "0.1.2-6-g3f0365f" {
		t.Fatalf("expected embedded version 0.1.2-6-g3f0365f, got %q", got)
	}
}

func TestEffectiveVersionUsesModuleVersionWhenEmbeddedDev(t *testing.T) {
	got := effectiveVersion("dev", "v0.1.2")
	if got != "0.1.2" {
		t.Fatalf("expected module version 0.1.2, got %q", got)
	}
}

func TestEffectiveVersionFallsBackToDev(t *testing.T) {
	got := effectiveVersion("dev", "(devel)")
	if got != "dev" {
		t.Fatalf("expected dev version, got %q", got)
	}
}
