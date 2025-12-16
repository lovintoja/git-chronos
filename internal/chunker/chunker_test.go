package chunker

import (
	"encoding/json"
	"testing"
)

func TestStripCodeFences(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"```json\n[]\n```", "[]"},
		{"```\n[]\n```", "[]"},
		{"[{\"commit_message\":\"test\",\"files_involved\":[]}]", "[{\"commit_message\":\"test\",\"files_involved\":[]}]"},
		{"  ```json\n[]\n```  ", "[]"},
	}
	for _, c := range cases {
		got := stripCodeFences(c.input)
		if got != c.expected {
			t.Errorf("stripCodeFences(%q) = %q, want %q", c.input, got, c.expected)
		}
	}
}

func TestCommitChunkUnmarshal(t *testing.T) {
	raw := `[{"commit_message":"feat: add auth","files_involved":["auth.go","auth_test.go"]}]`
	var chunks []CommitChunk
	if err := json.Unmarshal([]byte(raw), &chunks); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].CommitMessage != "feat: add auth" {
		t.Errorf("unexpected commit message: %s", chunks[0].CommitMessage)
	}
	if len(chunks[0].FilesInvolved) != 2 {
		t.Errorf("expected 2 files, got %d", len(chunks[0].FilesInvolved))
	}
}
