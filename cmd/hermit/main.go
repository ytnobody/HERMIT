package main

import (
	"bufio"
	"embed"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"

	"github.com/BurntSushi/toml"

	"github.com/ytnobody/hermit/internal/git"
	gh "github.com/ytnobody/hermit/internal/github"
	"github.com/ytnobody/hermit/internal/mcp"
	"github.com/ytnobody/hermit/internal/permissions"
	"github.com/ytnobody/hermit/internal/readiness"
	"github.com/ytnobody/hermit/internal/requirements"
	"github.com/ytnobody/hermit/internal/risk"
)

//go:embed templates/* templates/commands/*
var templateFS embed.FS

// RepoConfig holds the owner, repo, and optional label filter for a single
// repository entry in the [[repos]] array. Risk optionally overrides the
// top-level [risk] section for this repo only (multi-repo mode).
type RepoConfig struct {
	Owner string     `toml:"owner"`
	Repo  string     `toml:"repo"`
	Label string     `toml:"label"`
	Risk  RiskConfig `toml:"risk"`
}

// RiskConfig maps the [risk] (and per-repo [repos.risk]) harness.toml
// section onto internal/risk.Config. Any field left unset (empty slice or
// zero threshold) falls back to the parent level's value: [repos.risk] falls
// back to [risk], which itself falls back to risk.DefaultConfig().
type RiskConfig struct {
	HighPaths   []string `toml:"high_paths"`
	MediumPaths []string `toml:"medium_paths"`
	// ExcludePaths lists path prefixes (e.g. scaffold/template directories)
	// that never contribute a path-based HIGH/MEDIUM signal, even if they
	// also fall under a HighPaths/MediumPaths prefix. See risk.Config.
	ExcludePaths        []string `toml:"exclude_paths"`
	HighFileThreshold   int      `toml:"high_file_threshold"`
	HighLineThreshold   int      `toml:"high_line_threshold"`
	MediumFileThreshold int      `toml:"medium_file_threshold"`
	MediumLineThreshold int      `toml:"medium_line_threshold"`
	// RequireHumanApproval puts merge_pr into "warm-up mode" (see
	// risk.Config.RequireHumanApproval). It is read only from the top-level
	// [risk] section by resolveRiskConfig below; a value set under a
	// [[repos]] entry's [repos.risk] sub-table is intentionally ignored —
	// this flag has no per-repo override.
	RequireHumanApproval bool `toml:"require_human_approval"`
}

// toRiskConfig converts a RiskConfig (TOML shape) into a risk.Config.
func (r RiskConfig) toRiskConfig() risk.Config {
	return risk.Config{
		HighPaths:            r.HighPaths,
		MediumPaths:          r.MediumPaths,
		ExcludePaths:         r.ExcludePaths,
		HighFileThreshold:    r.HighFileThreshold,
		HighLineThreshold:    r.HighLineThreshold,
		MediumFileThreshold:  r.MediumFileThreshold,
		MediumLineThreshold:  r.MediumLineThreshold,
		RequireHumanApproval: r.RequireHumanApproval,
	}
}

type Config struct {
	GitHub struct {
		Owner              string `toml:"owner"`
		Repo               string `toml:"repo"`
		RateLimitThreshold int    `toml:"rate_limit_threshold"`
		DefaultBranch      string `toml:"default_branch"`
	} `toml:"github"`
	// Repos overrides the single [github] section when present.
	Repos []RepoConfig `toml:"repos"`
	Agent struct {
		MaxEngineers   int    `toml:"max_engineers"`
		Language       string `toml:"language"`
		BranchPrefix   string `toml:"branch_prefix"`
		LoopInterval   int    `toml:"loop_interval"`
		TriggerComment string `toml:"trigger_comment"`
	} `toml:"agent"`
	Model struct {
		Superintendent string `toml:"superintendent"`
		Engineer       string `toml:"engineer"`
		// Analyst configures the model used for the Analyst (PM) role, which
		// translates ambiguous human requirements gathered in the standing
		// requirements-hearing Issue into REQ-ID formatted requirements-doc
		// updates. Optional; when empty, resolveAnalystModel falls back to
		// Superintendent for backward compatibility with harness.toml files
		// written before the Analyst role existed.
		Analyst string `toml:"analyst"`
		// SuperintendentEffort/EngineerEffort/AnalystEffort configure the
		// Claude Agent tool's reasoning effort (e.g. low/medium/high/xhigh/max)
		// for the Superintendent, Engineer, and Analyst roles respectively.
		// All are optional; when empty, no effort is specified and the
		// caller's default applies.
		SuperintendentEffort string `toml:"superintendent_effort"`
		EngineerEffort       string `toml:"engineer_effort"`
		AnalystEffort        string `toml:"analyst_effort"`
	} `toml:"model"`
	Notification struct {
		WebhookURL string `toml:"webhook_url"`
		Type       string `toml:"type"`
	} `toml:"notification"`
	// Risk configures the risk-evaluation policy (see internal/risk.Config).
	// Any field left unset falls back to risk.DefaultConfig(). [[repos]]
	// entries may further override this via their own `risk` sub-table.
	Risk      RiskConfig `toml:"risk"`
	Readiness struct {
		// MinBodyLength is the minimum number of non-whitespace characters an
		// Issue body must contain to be considered ready for implementation.
		// Defaults to readiness.DefaultMinBodyLength when <= 0.
		MinBodyLength int `toml:"min_body_length"`
		// SkipAcceptanceCriteriaCheck disables the requirement that the Issue
		// body contain an acceptance-criteria-like section. Defaults to false
		// (i.e. the check is enabled) so the zero value is safe.
		SkipAcceptanceCriteriaCheck bool `toml:"skip_acceptance_criteria_check"`
		// Label is the GitHub label applied to Issues judged not ready, and
		// used to exclude them from list_issues. Defaults to
		// readiness.DefaultLabel when empty.
		Label string `toml:"label"`
	} `toml:"readiness"`
	Requirements struct {
		// Doc is the path (relative to the project root) to the requirements
		// document parsed for "## REQ-xxx: ..." blocks. Defaults to
		// "REQUIREMENTS.md" when empty.
		Doc string `toml:"doc"`
		// TestCommand is a shell command template used to check whether a
		// given requirement's test exists and passes. "{req_id}" is
		// substituted with the requirement's ID, e.g.:
		//   test_command = "go test ./... -run '^{req_id}' -v"
		// The template's output must include "=== RUN" markers (i.e.
		// `go test -v` style output) for every test that matched, so the
		// reconcile sweep can distinguish "no such test" from "test failed".
		TestCommand string `toml:"test_command"`
		// Paths lists candidate requirements-document locations (relative to
		// the project root); the project is considered to have a
		// requirements document if any one of them exists. Checked once at
		// `hermit serve` startup (see runRequirementsHearingCheck) — this is
		// Issue #104's precondition gate, distinct from (but complementary
		// to) the Doc field above used by the #106 reconcile sweep.
		//
		// Default when empty: falls back to Doc (if set), otherwise to
		// requirements.DefaultHearingPaths ("REQUIREMENTS.md" and
		// "docs/requirements.md"). This check is enabled by default — an
		// unconfigured [requirements] section is NOT a no-op — so every
		// project gets the guard against missing requirements docs without
		// needing to opt in.
		Paths []string `toml:"paths"`
	} `toml:"requirements"`
}

// resolveRiskConfig builds the effective default risk.Config (harness.toml's
// [risk] section layered over risk.DefaultConfig()) and, in multi-repo mode,
// a map of per-repo overrides keyed by "owner/repo" (layered over the
// resolved default).
//
// RequireHumanApproval is applied only from the top-level [risk] section and
// propagated as-is to every per-repo entry: risk.Merge deliberately never
// cascades this field (see its doc comment), so it must be set explicitly
// here to take effect, and any [repos.risk].require_human_approval value is
// intentionally never consulted — this flag is global-only, by design.
func resolveRiskConfig(cfg Config) (risk.Config, map[string]risk.Config) {
	def := risk.Merge(risk.DefaultConfig(), cfg.Risk.toRiskConfig())
	def.RequireHumanApproval = cfg.Risk.RequireHumanApproval

	var repoConfigs map[string]risk.Config
	for _, r := range cfg.Repos {
		merged := risk.Merge(def, r.Risk.toRiskConfig())
		merged.RequireHumanApproval = def.RequireHumanApproval
		if repoConfigs == nil {
			repoConfigs = make(map[string]risk.Config, len(cfg.Repos))
		}
		repoConfigs[r.Owner+"/"+r.Repo] = merged
	}
	return def, repoConfigs
}

// ModelPreset defines superintendent/engineer/analyst model combinations.
// SuperintendentEffort/EngineerEffort/AnalystEffort are optional
// reasoning-effort defaults (low/medium/high/xhigh/max) applied for each
// role; an empty value means no effort is specified and the caller's default
// applies. Analyst defaults to a higher-tier model than Engineer because
// misinterpreting ambiguous human language into REQ-ID requirements cascades
// directly into implementation effort downstream (see issue #107).
type ModelPreset struct {
	Superintendent       string
	Engineer             string
	Analyst              string
	SuperintendentEffort string
	EngineerEffort       string
	AnalystEffort        string
	Description          string
}

var modelPresets = map[string]ModelPreset{
	"claude": {
		Superintendent: "claude-sonnet-5",
		Engineer:       "claude-sonnet-5",
		Analyst:        "claude-opus-4-8",
		AnalystEffort:  "high",
		Description:    "Sonnet for Superintendent/Engineer, Opus for Analyst (balanced)",
	},
	"claude-cheap": {
		Superintendent: "claude-sonnet-5",
		Engineer:       "claude-haiku-4-5-20251001",
		Analyst:        "claude-sonnet-5",
		Description:    "Sonnet for Superintendent/Analyst, Haiku for Engineers (cost-optimized)",
	},
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}
	switch os.Args[1] {
	case "serve":
		cmdServe()
	case "install":
		cmdInstall()
	case "init":
		cmdInit()
	case "pause":
		cmdPause()
	case "resume":
		cmdResume()
	case "quit":
		cmdQuit()
	case "status":
		cmdStatus()
	case "use":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "Usage: hermit use <preset>")
			fmt.Fprintln(os.Stderr, "Available presets:")
			for name, p := range modelPresets {
				fmt.Fprintf(os.Stderr, "  %-20s %s\n", name, p.Description)
			}
			os.Exit(1)
		}
		cmdUse(os.Args[2])
	case "version":
		cmdVersion()
	case "upgrade":
		cmdUpgrade()
	case "cleanup":
		cmdCleanup()
	case "doctor":
		cmdDoctor()
	case "dry-run":
		cmdDryRun()
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n", os.Args[1])
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "Usage: hermit <serve|install|init|pause|resume|quit|status|use|version|upgrade|cleanup|doctor|dry-run>")
}

const pauseFile = ".hermit-paused"

// quitFile is a terminal flag file: unlike pauseFile (which is meant to be
// resumed via `hermit resume`), quitFile signals that the Superintendent's
// `/loop` should stop entirely — the loop must not call ScheduleWakeup again
// once this file is present. There is no `hermit unquit`; starting a fresh
// `/hermit` run again is the intended way to resume autonomous operation
// after a quit.
const quitFile = ".hermit-quit"

func cmdPause() {
	f, err := os.Create(pauseFile)
	if err != nil {
		fatal(err.Error())
	}
	f.Close()
	fmt.Println("⏸  Autonomous operation paused. To resume: hermit resume")
}

func cmdResume() {
	if err := os.Remove(pauseFile); err != nil {
		if os.IsNotExist(err) {
			fmt.Println("Autonomous operation is already running.")
			return
		}
		fatal(err.Error())
	}
	fmt.Println("▶  Autonomous operation resumed.")
}

func cmdQuit() {
	f, err := os.Create(quitFile)
	if err != nil {
		fatal(err.Error())
	}
	f.Close()
	fmt.Println("⏹  Autonomous operation quit requested. The Superintendent loop will stop at the start of its next cycle and will not reschedule itself.")
}

func cmdStatus() {
	if _, err := os.Stat(quitFile); err == nil {
		fmt.Println("⏹  quit requested (loop will stop)")
		return
	}
	if _, err := os.Stat(pauseFile); err == nil {
		fmt.Println("⏸  paused")
	} else {
		fmt.Println("▶  running")
	}
}

func cmdUse(presetName string) {
	preset, ok := modelPresets[presetName]
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown preset %q\n", presetName)
		fmt.Fprintln(os.Stderr, "Available presets:")
		for name, p := range modelPresets {
			fmt.Fprintf(os.Stderr, "  %-20s %s\n", name, p.Description)
		}
		os.Exit(1)
	}

	const harnessFile = "harness.toml"
	data, err := os.ReadFile(harnessFile)
	if err != nil {
		fatal("failed to read harness.toml: " + err.Error())
	}

	var cfg map[string]any
	if _, err := toml.Decode(string(data), &cfg); err != nil {
		fatal("failed to parse harness.toml: " + err.Error())
	}

	modelSection, _ := cfg["model"].(map[string]any)
	if modelSection == nil {
		modelSection = make(map[string]any)
	}
	modelSection["superintendent"] = preset.Superintendent
	modelSection["engineer"] = preset.Engineer
	modelSection["analyst"] = preset.Analyst
	if preset.SuperintendentEffort != "" {
		modelSection["superintendent_effort"] = preset.SuperintendentEffort
	}
	if preset.EngineerEffort != "" {
		modelSection["engineer_effort"] = preset.EngineerEffort
	}
	if preset.AnalystEffort != "" {
		modelSection["analyst_effort"] = preset.AnalystEffort
	}
	cfg["model"] = modelSection

	f, err := os.Create(harnessFile)
	if err != nil {
		fatal(err.Error())
	}
	defer f.Close()
	if err := toml.NewEncoder(f).Encode(cfg); err != nil {
		fatal("failed to write harness.toml: " + err.Error())
	}

	fmt.Printf("✓ preset %q applied\n", presetName)
	fmt.Printf("  superintendent: %s\n", preset.Superintendent)
	fmt.Printf("  engineer:       %s\n", preset.Engineer)
	fmt.Printf("  analyst:        %s\n", preset.Analyst)
	if preset.SuperintendentEffort != "" {
		fmt.Printf("  superintendent_effort: %s\n", preset.SuperintendentEffort)
	}
	if preset.EngineerEffort != "" {
		fmt.Printf("  engineer_effort:       %s\n", preset.EngineerEffort)
	}
	if preset.AnalystEffort != "" {
		fmt.Printf("  analyst_effort:        %s\n", preset.AnalystEffort)
	}
}

func githubToken() string {
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		return token
	}
	out, err := exec.Command("gh", "auth", "token").Output()
	if err != nil {
		fmt.Fprintln(os.Stderr, "warn: GITHUB_TOKEN is not set and gh auth token also failed. GitHub API calls will result in authentication errors.")
		return ""
	}
	return strings.TrimSpace(string(out))
}

// resolveBranchPrefix determines the branch prefix to use for worktrees.
// Priority:
//  1. cfg.Agent.BranchPrefix if set in harness.toml
//  2. "hermit/<gh_login>" if gh CLI is available
//  3. "hermit" as fallback (legacy format)
func resolveBranchPrefix(cfg Config) string {
	if cfg.Agent.BranchPrefix != "" {
		return cfg.Agent.BranchPrefix
	}
	login, err := gh.GetGitHubLogin()
	if err != nil {
		fmt.Fprintln(os.Stderr, "warn: could not retrieve GitHub login name. Using fallback branch prefix \"hermit\":", err)
		return "hermit"
	}
	return "hermit/" + login
}

// resolveAnalystModel returns the model configured for the Analyst role.
// Falls back to the Superintendent model when [model].analyst is unset, so
// that harness.toml files written before the Analyst role was introduced
// (issue #107) keep working without modification.
func resolveAnalystModel(cfg Config) string {
	if cfg.Model.Analyst != "" {
		return cfg.Model.Analyst
	}
	return cfg.Model.Superintendent
}

// resolveAnalystEffort returns the reasoning effort configured for the
// Analyst role. Falls back to the Superintendent's effort when
// [model].analyst_effort is unset, mirroring resolveAnalystModel's
// backward-compatibility fallback.
func resolveAnalystEffort(cfg Config) string {
	if cfg.Model.AnalystEffort != "" {
		return cfg.Model.AnalystEffort
	}
	return cfg.Model.SuperintendentEffort
}

func cmdServe() {
	// HERMIT_PROJECT_DIR explicitly pins the project root regardless of cwd.
	// cmdInstall writes this into the MCP server env so the correct harness.toml
	// is always used, even when Claude Code does not honour the cwd field.
	if projectDir := os.Getenv("HERMIT_PROJECT_DIR"); projectDir != "" {
		if err := os.Chdir(projectDir); err != nil {
			log.Printf("warn: HERMIT_PROJECT_DIR=%q: %v; falling back to cwd resolution", projectDir, err)
		}
	} else if _, err := os.Stat("harness.toml"); os.IsNotExist(err) {
		// No explicit project dir set and harness.toml not in cwd: fall back to
		// the binary's real location — Claude Code may not honour the cwd field
		// when spawning the MCP server.
		if execPath, err := os.Executable(); err == nil {
			if resolved, err := filepath.EvalSymlinks(execPath); err == nil {
				execPath = resolved
			}
			_ = os.Chdir(filepath.Dir(execPath))
		}
	}

	rootDir, _ := os.Getwd()

	// Detect legacy resources and warn before starting the MCP server.
	if branches := git.DetectLegacyBranches(); len(branches) > 0 {
		log.Printf("WARNING: %d legacy branch(es) detected: %v", len(branches), branches)
		log.Printf("         Run `hermit cleanup` to remove them.")
	}
	if zombies := git.DetectZombieWorktrees(); len(zombies) > 0 {
		log.Printf("WARNING: %d zombie worktree(s) detected: %v", len(zombies), zombies)
		log.Printf("         Run `hermit cleanup` to remove them.")
	}

	cfg := loadConfig()
	token := githubToken()
	client := gh.NewClient(token, cfg.GitHub.Owner, cfg.GitHub.Repo)
	prefix := resolveBranchPrefix(cfg)

	runRequirementsHearingCheck(rootDir, cfg, client)
	runRequirementsSweep(rootDir, cfg, client)

	// Convert []RepoConfig → []gh.RepoConfig for the MCP layer.
	var repos []gh.RepoConfig
	for _, r := range cfg.Repos {
		repos = append(repos, gh.RepoConfig{Owner: r.Owner, Repo: r.Repo, Label: r.Label})
	}

	readinessCfg := readiness.Config{
		MinBodyLength:             cfg.Readiness.MinBodyLength,
		RequireAcceptanceCriteria: !cfg.Readiness.SkipAcceptanceCriteriaCheck,
		Label:                     cfg.Readiness.Label,
	}

	defaultRiskCfg, repoRiskCfgs := resolveRiskConfig(cfg)

	requirementsCfg := mcp.RequirementsConfig{
		Doc:         resolveRequirementsDoc(cfg),
		TestCommand: cfg.Requirements.TestCommand,
	}

	model := mcp.ModelConfig{
		Superintendent:       cfg.Model.Superintendent,
		Engineer:             cfg.Model.Engineer,
		Analyst:              resolveAnalystModel(cfg),
		SuperintendentEffort: cfg.Model.SuperintendentEffort,
		EngineerEffort:       cfg.Model.EngineerEffort,
		AnalystEffort:        resolveAnalystEffort(cfg),
	}

	if err := mcp.Serve(client, cfg.GitHub.RateLimitThreshold, rootDir, prefix, cfg.Agent.LoopInterval, cfg.Notification.WebhookURL, cfg.Notification.Type, repos, cfg.Agent.TriggerComment, readinessCfg, defaultRiskCfg, repoRiskCfgs, model, requirementsCfg, cfg.Agent.MaxEngineers); err != nil {
		fatal(err.Error())
	}
}

// resolveHearingPaths returns the effective list of candidate
// requirements-document paths used by runRequirementsHearingCheck.
//
//  1. [requirements].paths, if explicitly set, is used as-is.
//  2. Otherwise, [requirements].doc, if set (even though it's primarily
//     consumed by the #106 reconcile sweep), is used as the sole candidate —
//     a project that has already configured a non-default doc path
//     shouldn't be told it's missing a requirements doc just because that
//     path isn't one of the hard-coded defaults.
//  3. Otherwise, requirements.DefaultHearingPaths is used.
func resolveHearingPaths(cfg Config) []string {
	if len(cfg.Requirements.Paths) > 0 {
		return cfg.Requirements.Paths
	}
	if cfg.Requirements.Doc != "" {
		return []string{cfg.Requirements.Doc}
	}
	return requirements.DefaultHearingPaths
}

// runRequirementsHearingCheck implements Issue #104's deterministic,
// idempotent precondition check: at `hermit serve` startup, verify that a
// requirements document exists at one of the configured (or default)
// candidate paths. If none exists, open exactly one "requirements hearing"
// Issue (deduped by the requirements.HearingLabel label) asking a human for
// the project's purpose, scope, acceptance criteria, and out-of-scope items.
//
// This check never depends on LLM judgment — existence is a plain os.Stat,
// and dedup is a plain GitHub label query — and it never blocks normal Issue
// processing: it only ever creates a separate, clearly-labeled Issue that
// list_issues excludes from the Engineer queue (see internal/mcp/tools.go).
//
// Like runRequirementsSweep, this is best-effort: any error is logged rather
// than fatal, since a hearing-check failure (e.g. a transient GitHub API
// error) must never prevent `hermit serve` from starting.
func runRequirementsHearingCheck(rootDir string, cfg Config, client *gh.Client) {
	paths := resolveHearingPaths(cfg)
	if requirements.DocExists(rootDir, paths) {
		return
	}

	created, err := requirements.EnsureHearingIssue(client)
	if err != nil {
		log.Printf("requirements hearing: %v", err)
		return
	}
	if created {
		log.Printf("requirements hearing: no requirements document found at %v; opened an issue labeled %q", paths, requirements.HearingLabel)
	}
}

// defaultRequirementsDoc is the requirements-document path used when
// [requirements].doc is not set in harness.toml.
const defaultRequirementsDoc = "REQUIREMENTS.md"

// resolveRequirementsDoc returns the effective requirements-document path
// (relative to the project root) used by both the startup reconcile sweep
// and the run_requirements_sweep MCP tool: [requirements].doc when set,
// otherwise defaultRequirementsDoc.
func resolveRequirementsDoc(cfg Config) string {
	if cfg.Requirements.Doc != "" {
		return cfg.Requirements.Doc
	}
	return defaultRequirementsDoc
}

// runRequirementsSweep runs the requirements reconcile sweep (Issue #106) at
// `hermit serve` startup: it parses the requirements document for "## REQ-xxx:"
// blocks, runs each requirement's test via the configured test_command, and
// opens (deduped) GitHub issues for requirements that are unimplemented,
// regressed, or whose text changed since the last sweep.
//
// This is intentionally best-effort: a project that hasn't adopted the
// requirements-doc format yet (no doc file, or no test_command configured)
// simply skips the sweep, and any sweep error is logged rather than fatal —
// it must never prevent `hermit serve` from starting.
//
// The actual sweep-running logic lives in requirements.RunReconcileSweep so
// that it is shared, unduplicated, with the run_requirements_sweep MCP tool
// (internal/mcp/tools.go) added by Issue #128 — that tool lets the
// Superintendent re-run this same sweep periodically (roughly hourly) from
// within its own patrol loop, rather than only once at process startup.
func runRequirementsSweep(rootDir string, cfg Config, client *gh.Client) {
	docPath := resolveRequirementsDoc(cfg)

	summary, err := requirements.RunReconcileSweep(rootDir, docPath, cfg.Requirements.TestCommand, requirements.NewGitHubIssueClient(client))
	if err != nil {
		log.Printf("requirements sweep: %v", err)
		return
	}
	if summary.Skipped {
		// Quiet skips (no doc yet, or a doc with no REQ-ID blocks yet) stay
		// silent at startup, matching the original behavior of this
		// function — most projects haven't adopted the requirements-doc
		// workflow yet, and logging about that on every `hermit serve`
		// start would just be noise. Non-quiet skips (e.g. a doc exists but
		// test_command isn't configured) are still worth a log line since
		// they indicate a fixable misconfiguration.
		if !summary.Quiet {
			log.Printf("requirements sweep: %s; skipping", summary.SkipReason)
		}
		return
	}

	for _, r := range summary.Results {
		if r.IssueCreated {
			log.Printf("requirements sweep: opened %s issue for %s", r.IssueKind, r.ReqID)
		}
	}
	log.Printf("requirements sweep: %d satisfied, %d unimplemented, %d regressed, %d skipped (manual), %d issue(s) opened",
		summary.Satisfied, summary.Unimplemented, summary.Regressed, summary.SkippedManual, summary.IssuesOpened)
}

func cmdCleanup() {
	fmt.Println("=== HERMIT Cleanup ===")

	branches := git.DetectLegacyBranches()
	if len(branches) == 0 {
		fmt.Println("Legacy branches: none")
	} else {
		fmt.Printf("Removing legacy branches: %v\n", branches)
		if err := git.CleanupLegacyBranches(branches); err != nil {
			fmt.Fprintf(os.Stderr, "warning: %v\n", err)
		} else {
			fmt.Printf("  Removed %d legacy branch(es).\n", len(branches))
		}
	}

	zombies := git.DetectZombieWorktrees()
	if len(zombies) == 0 {
		fmt.Println("Zombie worktrees: none")
	} else {
		fmt.Printf("Removing zombie worktrees: %v\n", zombies)
		cleaned := 0
		for _, path := range zombies {
			out, err := exec.Command("git", "worktree", "remove", "--force", path).CombinedOutput()
			if err != nil {
				fmt.Fprintf(os.Stderr, "  warning: failed to remove worktree %s: %s\n", path, strings.TrimSpace(string(out)))
			} else {
				cleaned++
			}
		}
		fmt.Printf("  Removed %d zombie worktree(s).\n", cleaned)
	}

	fmt.Println("Cleanup complete.")
}

func cmdInstall() {
	execPath, _ := os.Executable()
	if resolved, err := filepath.EvalSymlinks(execPath); err == nil {
		execPath = resolved
	}

	// cwd is where harness.toml lives, not the binary's directory.
	cwd, _ := os.Getwd()
	if _, err := os.Stat("harness.toml"); os.IsNotExist(err) {
		fatal("harness.toml not found. Please run `hermit install` from the project root.")
	}

	// Register hermit as a project-local MCP server through the Claude Code
	// CLI itself. Claude Code does NOT read MCP server definitions from
	// ~/.claude/settings.json — only from ~/.claude.json (managed via
	// `claude mcp add`) or a project .mcp.json file, so registration must go
	// through the officially supported `claude mcp add` command rather than
	// hand-writing Claude Code's internal config format.
	addArgs := []string{
		"mcp", "add", "hermit",
		"-s", "local",
		"-e", "GITHUB_TOKEN=${GITHUB_TOKEN}",
		"-e", "HERMIT_PROJECT_DIR=" + cwd,
		"--", execPath, "serve",
	}
	if out, err := exec.Command("claude", addArgs...).CombinedOutput(); err != nil {
		fatal("failed to register MCP server via `claude mcp add`: " + err.Error() + "\n" + string(out))
	}
	fmt.Println("✓ HERMIT MCP server registered with Claude Code (run `claude mcp list` to verify)")

	// Install slash commands into the project's .claude/commands/ directory.
	commandsDir := filepath.Join(cwd, ".claude", "commands")
	if err := os.MkdirAll(commandsDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "warn: could not create %s: %v\n", commandsDir, err)
	} else {
		cmdFiles := []string{"hermit.md", "hermit-pause.md", "hermit-resume.md", "hermit-quit.md"}
		allOK := true
		for _, name := range cmdFiles {
			src := "templates/commands/" + name
			data, err := templateFS.ReadFile(src)
			if err != nil {
				fmt.Fprintf(os.Stderr, "warn: could not read embedded %s: %v\n", src, err)
				allOK = false
				continue
			}
			dst := filepath.Join(commandsDir, name)
			if err := os.WriteFile(dst, data, 0o644); err != nil {
				fmt.Fprintf(os.Stderr, "warn: could not write %s: %v\n", dst, err)
				allOK = false
			}
		}
		if allOK {
			fmt.Println("✓ Slash commands installed to", commandsDir)
			fmt.Println("  Tip: run `git add .claude/commands/` to commit hermit's slash commands to version control.")
		}
	}

	// Symlink binary to ~/.local/bin so `hermit` is available in PATH.
	localBin := filepath.Join(os.Getenv("HOME"), ".local", "bin")
	if err := os.MkdirAll(localBin, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "warn: could not create %s: %v\n", localBin, err)
	} else {
		linkPath := filepath.Join(localBin, "hermit")
		if linkPath != execPath {
			_ = os.Remove(linkPath)
			if err := os.Symlink(execPath, linkPath); err != nil {
				fmt.Fprintf(os.Stderr, "warn: could not symlink hermit to %s: %v\n", linkPath, err)
			} else {
				fmt.Println("✓ hermit symlinked to", linkPath)
			}
		}
	}

	fmt.Println("")
	fmt.Println("⚠  Restart Claude Code to enable the MCP tools.")
}

func cmdInit() {
	sc := bufio.NewScanner(os.Stdin)

	owner := prompt(sc, "GitHub owner (org or user): ")
	repo := prompt(sc, "GitHub repo: ")
	lang := promptDefault(sc, "Language [ja/en] (default: ja): ", "ja")
	maxEngStr := promptDefault(sc, "Max parallel Engineers (default: 4): ", "4")
	maxEng, _ := strconv.Atoi(maxEngStr)
	if maxEng <= 0 {
		maxEng = 4
	}

	fmt.Println("Available model presets:")
	for name, p := range modelPresets {
		fmt.Printf("  %-20s %s\n", name, p.Description)
	}
	presetName := promptDefault(sc, "Model preset (default: claude): ", "claude")
	preset, ok := modelPresets[presetName]
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown preset %q, using 'claude'\n", presetName)
		preset = modelPresets["claude"]
	}

	supEffort := promptDefault(sc, "Superintendent reasoning effort [low/medium/high/xhigh/max] (optional, default: none): ", preset.SuperintendentEffort)
	engEffort := promptDefault(sc, "Engineer reasoning effort [low/medium/high/xhigh/max] (optional, default: none): ", preset.EngineerEffort)
	analystEffort := promptDefault(sc, "Analyst reasoning effort [low/medium/high/xhigh/max] (optional, press Enter for preset default): ", preset.AnalystEffort)

	type tmplData struct {
		Owner                string
		Repo                 string
		Language             string
		MaxEngineers         int
		SuperintendentModel  string
		EngineerModel        string
		AnalystModel         string
		SuperintendentEffort string
		EngineerEffort       string
		AnalystEffort        string
	}
	data := tmplData{
		Owner:                owner,
		Repo:                 repo,
		Language:             lang,
		MaxEngineers:         maxEng,
		SuperintendentModel:  preset.Superintendent,
		EngineerModel:        preset.Engineer,
		AnalystModel:         preset.Analyst,
		SuperintendentEffort: supEffort,
		EngineerEffort:       engEffort,
		AnalystEffort:        analystEffort,
	}

	writeTemplate("templates/harness.toml.tmpl", "harness.toml", data)
	writeTemplate("templates/CLAUDE.md.tmpl", "CLAUDE.md", struct {
		MaxEngineers         int
		ProjectCodingRules   string
		SuperintendentModel  string
		SuperintendentEffort string
		EngineerModel        string
		EngineerEffort       string
		AnalystModel         string
		AnalystEffort        string
	}{
		MaxEngineers:         maxEng,
		ProjectCodingRules:   "Describe your project-specific coding guidelines here.",
		SuperintendentModel:  preset.Superintendent,
		SuperintendentEffort: supEffort,
		EngineerModel:        preset.Engineer,
		EngineerEffort:       engEffort,
		AnalystModel:         preset.Analyst,
		AnalystEffort:        analystEffort,
	})

	// Generate .github/ISSUE_TEMPLATE/hermit-task.md for Issue creation guidance.
	if err := writeIssueTemplate(); err != nil {
		fatal("failed to write .github/ISSUE_TEMPLATE/hermit-task.md: " + err.Error())
	}

	// Generate .claude/settings.json so Claude Code runs autonomously without
	// confirmation prompts during the hermit loop.
	if err := writeClaudeSettings(); err != nil {
		fatal("failed to write .claude/settings.json: " + err.Error())
	}

	fmt.Println("\n✓ harness.toml and CLAUDE.md generated.")
	fmt.Println("✓ .github/ISSUE_TEMPLATE/hermit-task.md generated.")
	fmt.Println("✓ .claude/settings.json generated (autonomous operation mode).")
	fmt.Println("Next steps:")
	fmt.Println("  1. Edit the 'Coding Guidelines' section in CLAUDE.md")
	fmt.Println("  2. Set GITHUB_TOKEN and start `hermit serve`")
}

// writeClaudeSettings creates .claude/settings.json in the current directory
// with the comprehensive permission set required for autonomous hermit operation.
func writeClaudeSettings() error {
	if err := os.MkdirAll(".claude", 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(".claude", "settings.json"), permissions.DefaultSettingsJSON(), 0o644)
}

// writeIssueTemplate creates .github/ISSUE_TEMPLATE/hermit-task.md in the
// current directory so users can create well-structured Issues for HERMIT.
func writeIssueTemplate() error {
	dir := filepath.Join(".github", "ISSUE_TEMPLATE")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	content, err := templateFS.ReadFile("templates/hermit-task.md.tmpl")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "hermit-task.md"), content, 0o644)
}

func writeTemplate(tmplPath, outPath string, data any) {
	content, err := templateFS.ReadFile(tmplPath)
	if err != nil {
		fatal("template not found: " + tmplPath + ": " + err.Error())
	}
	t, err := template.New("").Option("missingkey=error").Parse(string(content))
	if err != nil {
		fatal("template parse error: " + err.Error())
	}
	f, err := os.Create(outPath)
	if err != nil {
		fatal(err.Error())
	}
	defer f.Close()
	if err := t.Execute(f, data); err != nil {
		fatal(err.Error())
	}
}

func loadConfig() Config {
	var cfg Config
	if _, err := toml.DecodeFile("harness.toml", &cfg); err != nil {
		if os.IsNotExist(err) {
			fatal("harness.toml not found. Please run `hermit init` from the project root.")
		}
		fatal("failed to load harness.toml: " + err.Error())
	}
	if cfg.Agent.LoopInterval <= 0 {
		cfg.Agent.LoopInterval = 270
	}
	if cfg.Readiness.MinBodyLength <= 0 {
		cfg.Readiness.MinBodyLength = readiness.DefaultMinBodyLength
	}
	if cfg.Readiness.Label == "" {
		cfg.Readiness.Label = readiness.DefaultLabel
	}
	if cfg.Agent.MaxEngineers <= 0 {
		cfg.Agent.MaxEngineers = defaultMaxEngineers
	}
	return cfg
}

// defaultMaxEngineers is the fallback [agent].max_engineers value (REQ-011)
// applied when harness.toml omits it or sets it to a non-positive number,
// matching the "hermit init" prompt's default (see promptDefault call for
// "Max parallel Engineers").
const defaultMaxEngineers = 4

func prompt(sc *bufio.Scanner, msg string) string {
	fmt.Print(msg)
	sc.Scan()
	return strings.TrimSpace(sc.Text())
}

func promptDefault(sc *bufio.Scanner, msg, def string) string {
	v := prompt(sc, msg)
	if v == "" {
		return def
	}
	return v
}

// fatalFunc is the function invoked by fatal(). Tests may replace it so that
// error paths can be exercised in-process without calling os.Exit.
var fatalFunc = func(msg string) {
	fmt.Fprintln(os.Stderr, "error:", msg)
	os.Exit(1)
}

func fatal(msg string) {
	fatalFunc(msg)
}
