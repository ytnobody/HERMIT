package risk

import (
	"strings"

	gh "github.com/ytnobody/hermit/internal/github"
)

type Level string

const (
	Low    Level = "LOW"
	Medium Level = "MEDIUM"
	High   Level = "HIGH"
)

var highPaths = []string{"cmd/", "go.mod", ".github/"}

func Evaluate(files []gh.PRFile, additions, deletions int) (Level, []string) {
	total := additions + deletions
	var reasons []string

	if len(files) >= 20 {
		reasons = append(reasons, "20 or more files changed")
	}
	if total >= 500 {
		reasons = append(reasons, "500 or more lines changed")
	}
	for _, f := range files {
		for _, p := range highPaths {
			if strings.HasPrefix(f.Filename, p) || f.Filename == p {
				reasons = append(reasons, f.Filename+" is in a high-risk path")
			}
		}
	}
	if len(reasons) > 0 {
		return High, reasons
	}

	if len(files) >= 10 {
		reasons = append(reasons, "10 or more files changed")
	}
	if total >= 200 {
		reasons = append(reasons, "200 or more lines changed")
	}
	for _, f := range files {
		if strings.HasPrefix(f.Filename, "internal/") {
			reasons = append(reasons, f.Filename+" has changes in internal core")
			break
		}
	}
	if len(reasons) > 0 {
		return Medium, reasons
	}

	return Low, nil
}
