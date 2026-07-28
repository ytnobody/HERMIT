package selfaudit

import "testing"

type fakeIssueClient struct {
	duplicates       map[string]int // normalized title -> existing issue number
	findDuplicateErr error
	createErr        error
	nextNum          int
	createdTitles    []string
	createdBodies    []string
}

func (f *fakeIssueClient) FindDuplicate(normalizedTitle string) (int, bool, error) {
	if f.findDuplicateErr != nil {
		return 0, false, f.findDuplicateErr
	}
	if num, ok := f.duplicates[normalizedTitle]; ok {
		return num, true, nil
	}
	return 0, false, nil
}

func (f *fakeIssueClient) CreateFindingIssue(title, body string) (int, error) {
	if f.createErr != nil {
		return 0, f.createErr
	}
	f.nextNum++
	f.createdTitles = append(f.createdTitles, title)
	f.createdBodies = append(f.createdBodies, body)
	return f.nextNum, nil
}

func TestFile_NewFinding_CreatesIssue(t *testing.T) {
	client := &fakeIssueClient{duplicates: map[string]int{}}
	findings := []Finding{{Title: "nil deref in foo.Bar", Body: "detail"}}

	summary, err := File(findings, client)
	if err != nil {
		t.Fatalf("File: %v", err)
	}
	if summary.IssuesOpened != 1 {
		t.Errorf("IssuesOpened = %d, want 1", summary.IssuesOpened)
	}
	if summary.Duplicates != 0 {
		t.Errorf("Duplicates = %d, want 0", summary.Duplicates)
	}
	if len(client.createdTitles) != 1 {
		t.Fatalf("expected 1 issue created, got %d", len(client.createdTitles))
	}
	if got := client.createdTitles[0]; got != TitlePrefix+" nil deref in foo.Bar" {
		t.Errorf("created title = %q, want prefixed title", got)
	}
	if len(summary.Results) != 1 || !summary.Results[0].IssueCreated || summary.Results[0].IssueNumber != 1 {
		t.Errorf("Results = %+v, want a single created result", summary.Results)
	}
}

func TestFile_DuplicateFinding_SkipsCreate(t *testing.T) {
	client := &fakeIssueClient{duplicates: map[string]int{"nil deref in foo bar": 42}}
	findings := []Finding{{Title: "nil deref in foo.Bar", Body: "detail"}}

	summary, err := File(findings, client)
	if err != nil {
		t.Fatalf("File: %v", err)
	}
	if summary.IssuesOpened != 0 {
		t.Errorf("IssuesOpened = %d, want 0", summary.IssuesOpened)
	}
	if summary.Duplicates != 1 {
		t.Errorf("Duplicates = %d, want 1", summary.Duplicates)
	}
	if len(client.createdTitles) != 0 {
		t.Errorf("expected no issue created for a duplicate, got %v", client.createdTitles)
	}
	if len(summary.Results) != 1 || !summary.Results[0].IsDuplicate || summary.Results[0].DuplicateOf != 42 {
		t.Errorf("Results = %+v, want a single duplicate result pointing at #42", summary.Results)
	}
}

func TestFile_MultipleFindings_MixedDuplicates(t *testing.T) {
	client := &fakeIssueClient{duplicates: map[string]int{"already filed": 7}}
	findings := []Finding{
		{Title: "already filed", Body: "b1"},
		{Title: "brand new bug", Body: "b2"},
	}

	summary, err := File(findings, client)
	if err != nil {
		t.Fatalf("File: %v", err)
	}
	if summary.FindingsReceived != 2 {
		t.Errorf("FindingsReceived = %d, want 2", summary.FindingsReceived)
	}
	if summary.IssuesOpened != 1 || summary.Duplicates != 1 {
		t.Errorf("IssuesOpened=%d Duplicates=%d, want 1 and 1", summary.IssuesOpened, summary.Duplicates)
	}
}

func TestFile_EmptyFindings_NoOp(t *testing.T) {
	client := &fakeIssueClient{duplicates: map[string]int{}}
	summary, err := File(nil, client)
	if err != nil {
		t.Fatalf("File: %v", err)
	}
	if summary.IssuesOpened != 0 || summary.Duplicates != 0 || summary.FindingsReceived != 0 {
		t.Errorf("Summary = %+v, want all-zero for no findings", summary)
	}
}

func TestFile_CreateError_ReturnsPartialSummaryAndError(t *testing.T) {
	client := &fakeIssueClient{duplicates: map[string]int{}, createErr: errBoom}
	findings := []Finding{{Title: "will fail", Body: "b"}}

	_, err := File(findings, client)
	if err == nil {
		t.Fatal("expected error from CreateFindingIssue to propagate")
	}
}

func TestFile_DuplicateCheckError_ReturnsError(t *testing.T) {
	client := &fakeIssueClient{findDuplicateErr: errBoom}
	findings := []Finding{{Title: "x", Body: "b"}}

	_, err := File(findings, client)
	if err == nil {
		t.Fatal("expected error from FindDuplicate to propagate")
	}
}

func TestNormalizeTitle_StripsPrefixAndPunctuation(t *testing.T) {
	cases := map[string]string{
		"[self-audit] nil deref in foo.Bar!": "nil deref in foo bar",
		"Nil Deref In Foo.Bar":               "nil deref in foo bar",
		"  extra   spaces  ":                 "extra spaces",
	}
	for in, want := range cases {
		if got := NormalizeTitle(in); got != want {
			t.Errorf("NormalizeTitle(%q) = %q, want %q", in, got, want)
		}
	}
}

var errBoom = &testError{"boom"}

type testError struct{ msg string }

func (e *testError) Error() string { return e.msg }
