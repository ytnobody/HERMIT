package risk

import (
	"fmt"
	"strings"

	gh "github.com/ytnobody/hermit/internal/github"
)

type Level string

const (
	Low    Level = "LOW"
	Medium Level = "MEDIUM"
	High   Level = "HIGH"
)

// Config holds the tunable thresholds and path lists used by Evaluate to
// classify a PR's risk level. It is intentionally decoupled from any
// particular configuration file format so it can be constructed directly in
// tests or built from harness.toml (see cmd/hermit for the TOML mapping).
type Config struct {
	HighPaths   []string `json:"high_paths"`
	MediumPaths []string `json:"medium_paths"`
	// ExcludePaths lists path prefixes that must never contribute a
	// path-based HIGH/MEDIUM signal, even if they also match a prefix in
	// HighPaths or MediumPaths. This is for scaffold/doc content that lives
	// under an otherwise risky directory (e.g. cmd/hermit/templates/ holds
	// template text, not executable cmd/ program logic) rather than a
	// one-off hardcoded exception in the matching logic.
	ExcludePaths        []string `json:"exclude_paths"`
	HighFileThreshold   int      `json:"high_file_threshold"`
	HighLineThreshold   int      `json:"high_line_threshold"`
	MediumFileThreshold int      `json:"medium_file_threshold"`
	MediumLineThreshold int      `json:"medium_line_threshold"`
	// RequireHumanApproval, when true, puts HERMIT into "warm-up mode": the
	// merge_pr MCP tool always blocks auto-merge (the same code path already
	// used for HIGH-risk PRs), regardless of the risk level Evaluate/
	// EvaluateWithConfig actually computes for a given PR. It does not affect
	// risk-level computation itself — Evaluate and EvaluateWithConfig ignore
	// this field entirely; it is purely a merge_pr gating decision.
	//
	// This is intentionally a single global flag with no per-repo override:
	// Merge (below) deliberately does not cascade this field from override
	// onto base, both because a bool zero value cannot distinguish "unset"
	// from "explicitly false" the way an empty slice or a zero threshold can,
	// and because the flag is meant to apply uniformly across every repo in
	// multi-repo mode. Callers that need to combine a harness.toml [risk]
	// section with the built-in defaults must set this field explicitly
	// after calling Merge (see cmd/hermit's resolveRiskConfig).
	RequireHumanApproval bool `json:"require_human_approval"`
}

// DefaultConfig returns the built-in risk policy that HERMIT has always
// used. It is the fallback applied whenever a value is left unspecified in
// harness.toml, preserving backward compatibility for existing projects.
func DefaultConfig() Config {
	return Config{
		HighPaths:   []string{"cmd/", "go.mod", ".github/"},
		MediumPaths: []string{"internal/"},
		// cmd/hermit/templates/ holds scaffold/doc content (CLAUDE.md.tmpl,
		// harness.toml.tmpl, command markdown copied into user projects) that
		// happens to live under the cmd/ prefix but carries none of the risk
		// of actual CLI-entrypoint program logic, so it's excluded from
		// path-based matching by default.
		ExcludePaths:         []string{"cmd/hermit/templates/"},
		HighFileThreshold:    20,
		HighLineThreshold:    500,
		MediumFileThreshold:  10,
		MediumLineThreshold:  200,
		RequireHumanApproval: false,
	}
}

// Merge returns a copy of base with any non-zero-value field from override
// applied on top. It is used to layer harness.toml's [risk] section over the
// built-in defaults, and again to layer a per-repo [repos.risk] override over
// the resulting effective config. Fields left unset (empty slice or zero
// int) in override are left untouched, i.e. they fall back to base.
//
// RequireHumanApproval is deliberately excluded from this cascade — see its
// doc comment on Config. result.RequireHumanApproval always retains base's
// value; callers that need to apply harness.toml's top-level [risk]
// .require_human_approval must set it explicitly after calling Merge.
func Merge(base, override Config) Config {
	result := base
	if len(override.HighPaths) > 0 {
		result.HighPaths = override.HighPaths
	}
	if len(override.MediumPaths) > 0 {
		result.MediumPaths = override.MediumPaths
	}
	if len(override.ExcludePaths) > 0 {
		result.ExcludePaths = override.ExcludePaths
	}
	if override.HighFileThreshold > 0 {
		result.HighFileThreshold = override.HighFileThreshold
	}
	if override.HighLineThreshold > 0 {
		result.HighLineThreshold = override.HighLineThreshold
	}
	if override.MediumFileThreshold > 0 {
		result.MediumFileThreshold = override.MediumFileThreshold
	}
	if override.MediumLineThreshold > 0 {
		result.MediumLineThreshold = override.MediumLineThreshold
	}
	return result
}

// Evaluate classifies a PR's risk level using the built-in default policy.
// It is kept as a stable, zero-config entry point for existing callers; use
// EvaluateWithConfig to apply a policy sourced from harness.toml.
func Evaluate(files []gh.PRFile, additions, deletions int) (Level, []string) {
	return EvaluateWithConfig(files, additions, deletions, DefaultConfig())
}

// EvaluateWithConfig classifies a PR's risk level using the supplied Config
// instead of the built-in defaults, allowing per-project or per-repo risk
// policies.
func EvaluateWithConfig(files []gh.PRFile, additions, deletions int, cfg Config) (Level, []string) {
	total := additions + deletions
	var reasons []string

	if cfg.HighFileThreshold > 0 && len(files) >= cfg.HighFileThreshold {
		reasons = append(reasons, fmt.Sprintf("%d or more files changed", cfg.HighFileThreshold))
	}
	if cfg.HighLineThreshold > 0 && total >= cfg.HighLineThreshold {
		reasons = append(reasons, fmt.Sprintf("%d or more lines changed", cfg.HighLineThreshold))
	}
	for _, f := range files {
		if isPathExcludedFromMatching(f.Filename, cfg) {
			continue
		}
		for _, p := range cfg.HighPaths {
			if strings.HasPrefix(f.Filename, p) || f.Filename == p {
				reasons = append(reasons, f.Filename+" is in a high-risk path")
			}
		}
	}
	if len(reasons) > 0 {
		return High, reasons
	}

	if cfg.MediumFileThreshold > 0 && len(files) >= cfg.MediumFileThreshold {
		reasons = append(reasons, fmt.Sprintf("%d or more files changed", cfg.MediumFileThreshold))
	}
	if cfg.MediumLineThreshold > 0 && total >= cfg.MediumLineThreshold {
		reasons = append(reasons, fmt.Sprintf("%d or more lines changed", cfg.MediumLineThreshold))
	}
	for _, f := range files {
		if isPathExcludedFromMatching(f.Filename, cfg) {
			continue
		}
		for _, p := range cfg.MediumPaths {
			if strings.HasPrefix(f.Filename, p) || f.Filename == p {
				reasons = append(reasons, f.Filename+" has changes in a medium-risk path")
				break
			}
		}
	}
	if len(reasons) > 0 {
		return Medium, reasons
	}

	return Low, nil
}

// isPathExcludedFromMatching reports whether filename must be skipped when
// evaluating cfg.HighPaths/cfg.MediumPaths prefix matches. Two categories are
// excluded:
//
//  1. Go test files (*_test.go): adding or modifying tests should never by
//     itself escalate risk, since tests reduce risk rather than increase it.
//     This is unconditional and not configurable via cfg, since it reflects
//     a general property of test files rather than a project-specific
//     policy choice.
//  2. Any path matching a cfg.ExcludePaths prefix, e.g. scaffold/template
//     content that happens to live under an otherwise high-risk directory.
//
// Line/file-count thresholds are unaffected by this exclusion: a diff that
// only touches excluded paths can still trigger HIGH/MEDIUM if it's large
// enough on its own.
func isPathExcludedFromMatching(filename string, cfg Config) bool {
	if strings.HasSuffix(filename, "_test.go") {
		return true
	}
	for _, p := range cfg.ExcludePaths {
		if strings.HasPrefix(filename, p) {
			return true
		}
	}
	return false
}
