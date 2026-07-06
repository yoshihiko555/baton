package main

import "testing"

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
