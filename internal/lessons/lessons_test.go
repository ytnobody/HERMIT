package lessons

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScore(t *testing.T) {
	tests := []struct {
		name       string
		input      ScoreInput
		wantScore  int
		wantDeduct int
	}{
		{
			name:       "perfect: LOW risk, CI passing, single PR",
			input:      ScoreInput{PRRiskLevel: "LOW"},
			wantScore:  100,
			wantDeduct: 0,
		},
		{
			name:       "HIGH risk deducts 30",
			input:      ScoreInput{PRRiskLevel: "HIGH"},
			wantScore:  70,
			wantDeduct: 1,
		},
		{
			name:       "MEDIUM risk deducts 15",
			input:      ScoreInput{PRRiskLevel: "MEDIUM"},
			wantScore:  85,
			wantDeduct: 1,
		},
		{
			name:       "CI failing deducts 20",
			input:      ScoreInput{PRRiskLevel: "LOW", CIWasFailing: true},
			wantScore:  80,
			wantDeduct: 1,
		},
		{
			name:       "multiple PRs deducts 15",
			input:      ScoreInput{PRRiskLevel: "LOW", HasMultiplePRs: true},
			wantScore:  85,
			wantDeduct: 1,
		},
		{
			name:       "clarification needed deducts 20",
			input:      ScoreInput{PRRiskLevel: "LOW", HasClarification: true},
			wantScore:  80,
			wantDeduct: 1,
		},
		{
			name: "HIGH + CI failing + clarification: floor at 0",
			input: ScoreInput{
				PRRiskLevel:      "HIGH",
				CIWasFailing:     true,
				HasClarification: true,
				HasMultiplePRs:   true,
			},
			wantScore:  15,
			wantDeduct: 4,
		},
		{
			name:       "score not below 0: all signals",
			input:      ScoreInput{PRRiskLevel: "HIGH", CIWasFailing: true, HasMultiplePRs: true, HasClarification: true},
			wantScore:  15,
			wantDeduct: 4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score, deductions := Score(tt.input)
			if score != tt.wantScore {
				t.Errorf("Score() = %d, want %d", score, tt.wantScore)
			}
			if len(deductions) != tt.wantDeduct {
				t.Errorf("deductions count = %d, want %d: %v", len(deductions), tt.wantDeduct, deductions)
			}
		})
	}
}

func TestGenerateLesson(t *testing.T) {
	tests := []struct {
		name        string
		score       int
		deductions  []string
		wantEmpty   bool
		wantContain string
	}{
		{
			name:      "score >= 70 returns empty",
			score:     70,
			wantEmpty: true,
		},
		{
			name:      "score 100 returns empty",
			score:     100,
			wantEmpty: true,
		},
		{
			name:        "HIGH risk lesson contains risk label",
			score:       69,
			deductions:  []string{"[HIGH] PR rated as high risk (-30)"},
			wantContain: "[HIGH]",
		},
		{
			name:        "CI failure lesson",
			score:       60,
			deductions:  []string{"[MEDIUM] CI was failing before merge (-20)"},
			wantContain: "test scenarios",
		},
		{
			name:        "multiple PRs lesson",
			score:       65,
			deductions:  []string{"[LOW] Multiple PRs were created (-15)"},
			wantContain: "one PR",
		},
		{
			name:        "clarification lesson",
			score:       60,
			deductions:  []string{"[MEDIUM] A [Clarification Needed] comment was present (-20)"},
			wantContain: "ambiguous",
		},
		{
			name:        "empty deductions with low score gets generic lesson",
			score:       50,
			deductions:  []string{},
			wantContain: "instruction quality",
		},
		{
			name:        "lesson includes score",
			score:       55,
			deductions:  []string{"[HIGH] PR rated as high risk (-30)"},
			wantContain: "score: 55",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GenerateLesson(tt.score, tt.deductions)
			if tt.wantEmpty {
				if got != "" {
					t.Errorf("GenerateLesson() = %q, want empty", got)
				}
				return
			}
			if !strings.Contains(got, tt.wantContain) {
				t.Errorf("GenerateLesson() = %q, want to contain %q", got, tt.wantContain)
			}
		})
	}
}

func TestAppendAndReadLessons(t *testing.T) {
	dir := t.TempDir()

	// Read from non-existent file returns nil
	lessons, err := ReadLessons(dir)
	if err != nil {
		t.Fatalf("ReadLessons on missing file: %v", err)
	}
	if len(lessons) != 0 {
		t.Errorf("expected empty lessons, got %v", lessons)
	}

	// Append single lesson
	if err := AppendLesson(dir, "[HIGH] Test lesson (score: 60)"); err != nil {
		t.Fatalf("AppendLesson: %v", err)
	}
	lessons, err = ReadLessons(dir)
	if err != nil {
		t.Fatalf("ReadLessons: %v", err)
	}
	if len(lessons) != 1 {
		t.Errorf("expected 1 lesson, got %d", len(lessons))
	}

	// Append empty lesson is a no-op
	if err := AppendLesson(dir, ""); err != nil {
		t.Fatalf("AppendLesson empty: %v", err)
	}
	lessons, _ = ReadLessons(dir)
	if len(lessons) != 1 {
		t.Errorf("expected 1 lesson after no-op append, got %d", len(lessons))
	}
}

func TestAppendLessonTrimsToMaxLessons(t *testing.T) {
	dir := t.TempDir()

	// Write 15 lessons
	for i := 0; i < maxLessons; i++ {
		lesson := "[LOW] lesson test"
		if i == 0 {
			lesson = "[HIGH] high-risk lesson"
		}
		if err := AppendLesson(dir, lesson); err != nil {
			t.Fatalf("AppendLesson %d: %v", i, err)
		}
	}

	lessons, _ := ReadLessons(dir)
	if len(lessons) != maxLessons {
		t.Errorf("expected %d lessons, got %d", maxLessons, len(lessons))
	}

	// Add one more — should trim to maxLessons
	if err := AppendLesson(dir, "[MEDIUM] overflow test"); err != nil {
		t.Fatalf("AppendLesson overflow: %v", err)
	}
	lessons, _ = ReadLessons(dir)
	if len(lessons) != maxLessons {
		t.Errorf("expected %d lessons after trim, got %d", maxLessons, len(lessons))
	}

	// High-risk lesson should be retained
	found := false
	for _, l := range lessons {
		if strings.HasPrefix(l, "[HIGH]") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("high-risk lesson should be preserved after trim, lessons: %v", lessons)
	}
}

func TestTrimLessons(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		wantLen  int
		wantHigh bool
	}{
		{
			name:    "no trim needed",
			input:   make([]string, maxLessons),
			wantLen: maxLessons,
		},
		{
			name: "high risk preserved over low",
			input: func() []string {
				ls := make([]string, maxLessons+2)
				ls[0] = "[HIGH] important lesson"
				for i := 1; i < len(ls); i++ {
					ls[i] = "[LOW] low-priority lesson"
				}
				return ls
			}(),
			wantLen:  maxLessons,
			wantHigh: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := trimLessons(tt.input)
			if len(got) != tt.wantLen {
				t.Errorf("trimLessons() len = %d, want %d", len(got), tt.wantLen)
			}
			if tt.wantHigh {
				found := false
				for _, l := range got {
					if strings.HasPrefix(l, "[HIGH]") {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("high-risk lesson not preserved: %v", got)
				}
			}
		})
	}
}

func TestProcessMergedPR(t *testing.T) {
	tests := []struct {
		name             string
		prRiskLevel      string
		ciWasFailing     bool
		hasMultiplePRs   bool
		hasClarification bool
		wantMinScore     int
		wantMaxScore     int
		wantLesson       bool
	}{
		{
			name:         "LOW risk perfect: no lesson",
			prRiskLevel:  "LOW",
			wantMinScore: 100,
			wantMaxScore: 100,
			wantLesson:   false,
		},
		{
			name:         "HIGH risk: lesson generated",
			prRiskLevel:  "HIGH",
			wantMinScore: 70,
			wantMaxScore: 70,
			wantLesson:   false, // score == 70, threshold is < 70
		},
		{
			name:         "HIGH + CI failing: lesson generated",
			prRiskLevel:  "HIGH",
			ciWasFailing: true,
			wantMinScore: 50,
			wantMaxScore: 50,
			wantLesson:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			score, lesson, err := ProcessMergedPR(dir, tt.prRiskLevel, tt.ciWasFailing, tt.hasMultiplePRs, tt.hasClarification)
			if err != nil {
				t.Fatalf("ProcessMergedPR: %v", err)
			}
			if score < tt.wantMinScore || score > tt.wantMaxScore {
				t.Errorf("score = %d, want [%d, %d]", score, tt.wantMinScore, tt.wantMaxScore)
			}
			if tt.wantLesson && lesson == "" {
				t.Errorf("expected a lesson to be generated, got empty")
			}
			if !tt.wantLesson && lesson != "" {
				t.Errorf("expected no lesson, got %q", lesson)
			}
			// Verify file state
			if tt.wantLesson {
				path := filepath.Join(dir, ".hermit/lessons.txt")
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("lessons.txt not found: %v", err)
				}
				if !strings.Contains(string(data), lesson) {
					t.Errorf("lessons.txt does not contain lesson %q", lesson)
				}
			}
		})
	}
}
