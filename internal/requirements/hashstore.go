package requirements

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// HashStore persists the last-seen content hash of each requirement so the
// sweep can detect when a requirement's text has changed since the previous
// run. This is *not* a satisfaction record — it never says whether a
// requirement is "done"; it only remembers enough to avoid re-firing a
// "review the test" issue every single sweep for a change that was already
// reported.
type HashStore interface {
	Load() (map[string]string, error)
	Save(map[string]string) error
}

// DefaultHashStorePath is the path, relative to the project root, where
// FileHashStore persists requirement hashes by default.
const DefaultHashStorePath = ".hermit/requirements-hashes.json"

// FileHashStore persists requirement hashes as JSON on disk.
type FileHashStore struct {
	Path string
}

// NewFileHashStore returns a FileHashStore rooted at dir, using
// DefaultHashStorePath.
func NewFileHashStore(dir string) FileHashStore {
	return FileHashStore{Path: filepath.Join(dir, DefaultHashStorePath)}
}

// Load reads the stored hash map. A missing file is not an error — it
// returns an empty map, since that's the expected state before the first
// sweep has ever run.
func (f FileHashStore) Load() (map[string]string, error) {
	data, err := os.ReadFile(f.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, err
	}
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	if m == nil {
		m = map[string]string{}
	}
	return m, nil
}

// Save writes the hash map to disk, creating parent directories as needed.
func (f FileHashStore) Save(hashes map[string]string) error {
	if err := os.MkdirAll(filepath.Dir(f.Path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(hashes, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(f.Path, data, 0o644)
}

// memHashStore is a trivial in-memory HashStore, useful for tests and for
// callers that intentionally don't want cross-run persistence.
type memHashStore struct {
	data map[string]string
}

// NewMemHashStore returns an in-memory HashStore starting empty.
func NewMemHashStore() HashStore {
	return &memHashStore{data: map[string]string{}}
}

func (m *memHashStore) Load() (map[string]string, error) {
	out := make(map[string]string, len(m.data))
	for k, v := range m.data {
		out[k] = v
	}
	return out, nil
}

func (m *memHashStore) Save(hashes map[string]string) error {
	m.data = make(map[string]string, len(hashes))
	for k, v := range hashes {
		m.data[k] = v
	}
	return nil
}
