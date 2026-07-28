package requirements

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// HashSchemeVersion identifies the algorithm used to compute Requirement.Hash
// (see specHash). It must be bumped whenever the set of fields that feed the
// hash changes, so HashStore can tell a genuine spec change apart from a
// hash produced by a since-retired scheme.
//
// Bumping this on its own is intentionally *not* enough to make old stored
// hashes compare unequal to new ones and fire review-test: Sweep checks the
// stored version against this constant and, on a mismatch, treats it as
// "nothing changed" for review-test purposes — it only recomputes and
// persists hashes under the new scheme (Issue #182's migration
// requirement). This avoids a scheme change (like #182's fix itself, which
// narrowed the hash to exclude 実装状況) causing every requirement to look
// "changed" and firing review-test for the entire document in one sweep.
const HashSchemeVersion = 2

// HashStore persists the last-seen content hash of each requirement (plus
// the scheme version those hashes were computed under) so the sweep can
// detect when a requirement's *spec* has changed since the previous run.
// This is *not* a satisfaction record — it never says whether a requirement
// is "done"; it only remembers enough to avoid re-firing a "review the test"
// issue every single sweep for a change that was already reported.
type HashStore interface {
	// Load returns the previously stored scheme version and hash map. A
	// store that has never been written returns version 0 (which never
	// equals a real HashSchemeVersion, so callers can detect "no prior
	// data" the same way they detect "old scheme") and an empty map.
	Load() (version int, hashes map[string]string, err error)
	// Save persists hashes under the given scheme version.
	Save(version int, hashes map[string]string) error
}

// DefaultHashStorePath is the path, relative to the project root, where
// FileHashStore persists requirement hashes by default.
const DefaultHashStorePath = ".hermit/requirements-hashes.json"

// fileHashStoreData is the on-disk JSON shape used by FileHashStore.
type fileHashStoreData struct {
	// Version is the HashSchemeVersion the Hashes below were computed
	// under. Absent/zero in files written before Issue #182 introduced
	// versioning, which is exactly the "unknown/old scheme" sentinel value
	// callers need.
	Version int               `json:"version"`
	Hashes  map[string]string `json:"hashes"`
}

// FileHashStore persists requirement hashes as JSON on disk.
type FileHashStore struct {
	Path string
}

// NewFileHashStore returns a FileHashStore rooted at dir, using
// DefaultHashStorePath.
func NewFileHashStore(dir string) FileHashStore {
	return FileHashStore{Path: filepath.Join(dir, DefaultHashStorePath)}
}

// Load reads the stored version and hash map. A missing file is not an
// error — it returns version 0 and an empty map, since that's the expected
// state before the first sweep has ever run.
func (f FileHashStore) Load() (int, map[string]string, error) {
	data, err := os.ReadFile(f.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, map[string]string{}, nil
		}
		return 0, nil, err
	}

	// Backward compatibility: files written before Issue #182 are a bare
	// {"REQ-001": "hash", ...} map with no "version"/"hashes" envelope.
	// Detect that shape and treat it as version 0 (unknown/old scheme) so
	// it goes through the same "recompute, don't fire" migration path as
	// any other scheme mismatch, instead of failing to unmarshal.
	var legacy map[string]string
	if err := json.Unmarshal(data, &legacy); err == nil {
		if _, isEnvelope := legacy["version"]; !isEnvelope {
			if legacy == nil {
				legacy = map[string]string{}
			}
			// Note: real envelope data can never reach this branch — its
			// "hashes" field is a JSON object, not a string, so unmarshaling
			// an envelope into map[string]string fails above and we never
			// get here with err == nil for that shape.
			return 0, legacy, nil
		}
	}

	var d fileHashStoreData
	if err := json.Unmarshal(data, &d); err != nil {
		return 0, nil, err
	}
	if d.Hashes == nil {
		d.Hashes = map[string]string{}
	}
	return d.Version, d.Hashes, nil
}

// Save writes the version and hash map to disk, creating parent directories
// as needed.
func (f FileHashStore) Save(version int, hashes map[string]string) error {
	if err := os.MkdirAll(filepath.Dir(f.Path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(fileHashStoreData{Version: version, Hashes: hashes}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(f.Path, data, 0o644)
}

// memHashStore is a trivial in-memory HashStore, useful for tests and for
// callers that intentionally don't want cross-run persistence.
type memHashStore struct {
	version int
	data    map[string]string
}

// NewMemHashStore returns an in-memory HashStore starting empty (version 0,
// as if never written).
func NewMemHashStore() HashStore {
	return &memHashStore{data: map[string]string{}}
}

func (m *memHashStore) Load() (int, map[string]string, error) {
	out := make(map[string]string, len(m.data))
	for k, v := range m.data {
		out[k] = v
	}
	return m.version, out, nil
}

func (m *memHashStore) Save(version int, hashes map[string]string) error {
	m.version = version
	m.data = make(map[string]string, len(hashes))
	for k, v := range hashes {
		m.data[k] = v
	}
	return nil
}
