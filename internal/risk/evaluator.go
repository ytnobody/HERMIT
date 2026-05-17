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
		reasons = append(reasons, "変更ファイル数が20以上")
	}
	if total >= 500 {
		reasons = append(reasons, "変更行数が500以上")
	}
	for _, f := range files {
		for _, p := range highPaths {
			if strings.HasPrefix(f.Filename, p) || f.Filename == p {
				reasons = append(reasons, f.Filename+" が高リスクパスに含まれる")
			}
		}
	}
	if len(reasons) > 0 {
		return High, reasons
	}

	if len(files) >= 10 {
		reasons = append(reasons, "変更ファイル数が10以上")
	}
	if total >= 200 {
		reasons = append(reasons, "変更行数が200以上")
	}
	for _, f := range files {
		if strings.HasPrefix(f.Filename, "internal/") {
			reasons = append(reasons, f.Filename+" がinternalコアに変更あり")
			break
		}
	}
	if len(reasons) > 0 {
		return Medium, reasons
	}

	return Low, nil
}
