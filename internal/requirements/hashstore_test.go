package requirements

import (
	"path/filepath"
	"testing"
)

func TestFileHashStore_LoadMissingFileReturnsEmptyMap(t *testing.T) {
	store := FileHashStore{Path: filepath.Join(t.TempDir(), "does-not-exist.json")}
	m, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(m) != 0 {
		t.Errorf("expected empty map, got %v", m)
	}
}

func TestFileHashStore_SaveThenLoadRoundTrip(t *testing.T) {
	store := FileHashStore{Path: filepath.Join(t.TempDir(), "nested", "hashes.json")}
	want := map[string]string{"REQ-001": "abc123", "REQ-002": "def456"}

	if err := store.Save(want); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
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
	if err := store.Save(map[string]string{"REQ-001": "x"}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got["REQ-001"] != "x" {
		t.Errorf("got %v", got)
	}
}
