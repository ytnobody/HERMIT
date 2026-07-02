// Package cihistory records whether CI was ever observed failing for a given
// PR, so that lessons scoring can penalize PRs that were merged despite an
// earlier CI failure (see internal/lessons.ScoreInput.CIWasFailing).
//
// History is persisted as marker files under .hermit/ci_failures/<pr_number>
// in the project root, so it survives across separate MCP tool invocations.
package cihistory

import (
	"fmt"
	"os"
	"path/filepath"
)

const historyDir = ".hermit/ci_failures"

// dirPath returns the absolute path to the CI failure history directory for
// the given project root.
func dirPath(rootDir string) string {
	return filepath.Join(rootDir, historyDir)
}

// filePath returns the marker file path for a given PR number.
func filePath(rootDir string, prNumber int) string {
	return filepath.Join(dirPath(rootDir), fmt.Sprintf("%d", prNumber))
}

// RecordFailure records that CI was observed failing for the given PR.
// rootDir is the project root directory. Safe to call multiple times.
func RecordFailure(rootDir string, prNumber int) error {
	if err := os.MkdirAll(dirPath(rootDir), 0o755); err != nil {
		return err
	}
	return os.WriteFile(filePath(rootDir, prNumber), []byte{}, 0o644)
}

// WasFailing reports whether CI was ever recorded as failing for the given
// PR. A missing history file is treated as "never failed" (false, nil error).
func WasFailing(rootDir string, prNumber int) (bool, error) {
	_, err := os.Stat(filePath(rootDir, prNumber))
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// ClearFailure removes any recorded CI failure history for the given PR.
// Intended to be called once the PR has been merged, so PR numbers cannot
// leak state into future (theoretical) reuse. Missing files are not an error.
func ClearFailure(rootDir string, prNumber int) error {
	err := os.Remove(filePath(rootDir, prNumber))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
