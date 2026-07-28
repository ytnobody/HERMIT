package requirements

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileHashStore_LoadMissingFileReturnsEmptyMap(t *testing.T) {
	store := FileHashStore{Path: filepath.Join(t.TempDir(), "does-not-exist.json")}
	version, m, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if version != 0 {
		t.Errorf("expected version 0 for a never-written store, got %d", version)
	}
	if len(m) != 0 {
		t.Errorf("expected empty map, got %v", m)
	}
}

func TestFileHashStore_SaveThenLoadRoundTrip(t *testing.T) {
	store := FileHashStore{Path: filepath.Join(t.TempDir(), "nested", "hashes.json")}
	want := map[string]string{"REQ-001": "abc123", "REQ-002": "def456"}

	if err := store.Save(HashSchemeVersion, want); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	version, got, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if version != HashSchemeVersion {
		t.Errorf("version = %d, want %d", version, HashSchemeVersion)
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("got[%q] = %q, want %q", k, got[k], v)
		}
	}
}

func TestFileHashStore_LoadLegacyBareMap_TreatedAsVersionZero(t *testing.T) {
	// Files written before Issue #182 introduced the version envelope are a
	// bare {"REQ-001": "hash"} map. Load must recognize this shape and
	// report version 0 (unknown/old scheme), not fail to parse it.
	path := filepath.Join(t.TempDir(), "legacy.json")
	if err := os.WriteFile(path, []byte(`{"REQ-001":"abc123"}`), 0o644); err != nil {
		t.Fatalf("writing legacy fixture: %v", err)
	}
	store := FileHashStore{Path: path}

	version, got, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if version != 0 {
		t.Errorf("version = %d, want 0 for legacy bare-map file", version)
	}
	if got["REQ-001"] != "abc123" {
		t.Errorf("got %v, want legacy hash preserved", got)
	}
}

func TestNewFileHashStore_UsesDefaultPath(t *testing.T) {
	dir := t.TempDir()
	store := NewFileHashStore(dir)
	want := filepath.Join(dir, ".hermit", "requirements-hashes.json")
	if store.Path != want {
		t.Errorf("Path = %q, want %q", store.Path, want)
	}
}

func TestMemHashStore_RoundTrip(t *testing.T) {
	store := NewMemHashStore()
	if err := store.Save(HashSchemeVersion, map[string]string{"REQ-001": "x"}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	version, got, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if version != HashSchemeVersion {
		t.Errorf("version = %d, want %d", version, HashSchemeVersion)
	}
	if got["REQ-001"] != "x" {
		t.Errorf("got %v", got)
	}
}
