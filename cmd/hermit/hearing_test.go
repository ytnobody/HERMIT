package main

import (
	"reflect"
	"testing"

	"github.com/ytnobody/hermit/internal/requirements"
)

// TestResolveHearingPaths_UsesConfiguredPaths verifies that an explicit
// [requirements].paths list is used as-is when set.
func TestResolveHearingPaths_UsesConfiguredPaths(t *testing.T) {
	cfg := Config{}
	cfg.Requirements.Paths = []string{"SPEC.md", "docs/spec.md"}
	cfg.Requirements.Doc = "REQUIREMENTS.md" // should be ignored when Paths is set

	got := resolveHearingPaths(cfg)
	want := []string{"SPEC.md", "docs/spec.md"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("resolveHearingPaths() = %v, want %v", got, want)
	}
}

// TestResolveHearingPaths_FallsBackToDoc verifies that a configured
// [requirements].doc (primarily used by the #106 reconcile sweep) is reused
// as the sole hearing-check candidate when paths is unset.
func TestResolveHearingPaths_FallsBackToDoc(t *testing.T) {
	cfg := Config{}
	cfg.Requirements.Doc = "SPEC.md"

	got := resolveHearingPaths(cfg)
	want := []string{"SPEC.md"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("resolveHearingPaths() = %v, want %v", got, want)
	}
}

// TestResolveHearingPaths_DefaultsWhenUnset verifies that resolveHearingPaths
// falls back to requirements.DefaultHearingPaths — i.e. the hearing check is
// enabled by default, not a no-op — when [requirements] is entirely absent.
func TestResolveHearingPaths_DefaultsWhenUnset(t *testing.T) {
	cfg := Config{}

	got := resolveHearingPaths(cfg)
	if !reflect.DeepEqual(got, requirements.DefaultHearingPaths) {
		t.Errorf("resolveHearingPaths() = %v, want default %v", got, requirements.DefaultHearingPaths)
	}
}
