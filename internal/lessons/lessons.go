package lessons

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	lessonsFile = ".hermit/lessons.txt"
	maxLessons  = 15
)

type RiskLabel string

const (
	RiskHigh   RiskLabel = "[高]"
	RiskMedium RiskLabel = "[中]"
	RiskLow    RiskLabel = "[低]"
)

// ScoreInput holds the signals used to score an issue's instruction quality.
type ScoreInput struct {
	PRRiskLevel    string // "HIGH", "MEDIUM", "LOW"
	CIWasFailing   bool
	HasMultiplePRs bool
	HasClarification bool
}

// Score returns a 0–100 quality score and the list of deductions applied.
func Score(input ScoreInput) (int, []string) {
	score := 100
	var deductions []string

	switch strings.ToUpper(input.PRRiskLevel) {
	case "HIGH":
		score -= 30
		deductions = append(deductions, fmt.Sprintf("%s PRが高リスク判定 (-30)", RiskHigh))
	case "MEDIUM":
		score -= 15
		deductions = append(deductions, fmt.Sprintf("%s PRが中リスク判定 (-15)", RiskMedium))
	}

	if input.CIWasFailing {
		score -= 20
		deductions = append(deductions, fmt.Sprintf("%s マージ前にCIが失敗していた (-20)", RiskMedium))
	}

	if input.HasMultiplePRs {
		score -= 15
		deductions = append(deductions, fmt.Sprintf("%s PRが複数作成された (-15)", RiskLow))
	}

	if input.HasClarification {
		score -= 20
		deductions = append(deductions, fmt.Sprintf("%s [Clarification Needed]コメントが存在した (-20)", RiskMedium))
	}

	if score < 0 {
		score = 0
	}
	return score, deductions
}

// GenerateLesson returns a single-line Japanese lesson for scores below 70.
// Returns empty string if score >= 70 (no lesson needed).
func GenerateLesson(score int, deductions []string) string {
	if score >= 70 {
		return ""
	}

	var parts []string
	for _, d := range deductions {
		if strings.HasPrefix(d, string(RiskHigh)) {
			parts = append(parts, "Issue指示をより詳細に記述し、変更スコープを限定する")
		} else if strings.Contains(d, "CI") {
			parts = append(parts, "実装前にテストシナリオを明示する")
		} else if strings.Contains(d, "複数") {
			parts = append(parts, "Issue1件につきPR1件となるよう指示を具体化する")
		} else if strings.Contains(d, "Clarification") {
			parts = append(parts, "曖昧な要件を事前に解決してからIssueを発行する")
		} else if strings.HasPrefix(d, string(RiskMedium)) {
			parts = append(parts, "変更範囲が広がらないよう実装境界を指定する")
		}
	}

	if len(parts) == 0 {
		parts = append(parts, "指示品質を見直し、要件をより明確に記述する")
	}

	label := label(deductions)
	return fmt.Sprintf("%s %s（採点: %d点）", label, strings.Join(uniqueStrings(parts), "、"), score)
}

func label(deductions []string) RiskLabel {
	for _, d := range deductions {
		if strings.HasPrefix(d, string(RiskHigh)) {
			return RiskHigh
		}
	}
	for _, d := range deductions {
		if strings.HasPrefix(d, string(RiskMedium)) {
			return RiskMedium
		}
	}
	return RiskLow
}

func uniqueStrings(ss []string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, s := range ss {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// AppendLesson appends a lesson to .hermit/lessons.txt, trimming to maxLessons entries.
// dir is the project root directory.
func AppendLesson(dir, lesson string) error {
	if lesson == "" {
		return nil
	}
	path := filepath.Join(dir, lessonsFile)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	existing, _ := ReadLessons(dir)
	existing = append(existing, lesson)

	if len(existing) > maxLessons {
		existing = trimLessons(existing)
	}

	content := strings.Join(existing, "\n") + "\n"
	return os.WriteFile(path, []byte(content), 0o644)
}

// trimLessons keeps up to maxLessons entries, preserving all high-risk lessons
// and filling the rest with the most recent non-high entries.
func trimLessons(ls []string) []string {
	var high, other []string
	for _, l := range ls {
		if strings.HasPrefix(l, string(RiskHigh)) {
			high = append(high, l)
		} else {
			other = append(other, l)
		}
	}

	// Allocate slots: all high-risk first, then fill remaining with most-recent others
	if len(high) >= maxLessons {
		return high[len(high)-maxLessons:]
	}
	remaining := maxLessons - len(high)
	if len(other) > remaining {
		other = other[len(other)-remaining:]
	}
	return append(high, other...)
}

// ReadLessons reads all lessons from .hermit/lessons.txt.
func ReadLessons(dir string) ([]string, error) {
	path := filepath.Join(dir, lessonsFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var lessons []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			lessons = append(lessons, line)
		}
	}
	return lessons, nil
}

// ProcessMergedPR scores the merged PR and saves a lesson if quality is low.
// dir is the project root directory.
func ProcessMergedPR(dir, prRiskLevel string, ciWasFailing, hasMultiplePRs, hasClarification bool) (int, string, error) {
	input := ScoreInput{
		PRRiskLevel:      prRiskLevel,
		CIWasFailing:     ciWasFailing,
		HasMultiplePRs:   hasMultiplePRs,
		HasClarification: hasClarification,
	}
	score, deductions := Score(input)
	lesson := GenerateLesson(score, deductions)
	if err := AppendLesson(dir, lesson); err != nil {
		return score, lesson, err
	}
	return score, lesson, nil
}
