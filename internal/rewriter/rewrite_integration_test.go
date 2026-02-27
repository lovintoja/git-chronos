package rewriter_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"git-chronos/internal/chunker"
	"git-chronos/internal/rewriter"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// initRepo creates a bare-bones git repo in dir with a user identity configured,
// then creates a single empty initial commit so HEAD is valid.
func initRepo(t *testing.T, dir string) *git.Repository {
	t.Helper()

	r, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("git.PlainInit: %v", err)
	}

	// Set a local user identity by writing directly to .git/config so commits
	// don't fail on machines with no global git config.
	gitConfigContent := "[user]\n\tname = Test User\n\temail = test@example.com\n"
	gitConfigPath := filepath.Join(dir, ".git", "config")
	existing, _ := os.ReadFile(gitConfigPath)
	if err := os.WriteFile(gitConfigPath, append(existing, []byte(gitConfigContent)...), 0644); err != nil {
		t.Fatalf("write .git/config: %v", err)
	}

	// Create an initial empty commit so HEAD resolves.
	wt, err := r.Worktree()
	if err != nil {
		t.Fatalf("r.Worktree: %v", err)
	}

	placeholder := filepath.Join(dir, ".gitkeep")
	if err := os.WriteFile(placeholder, []byte(""), 0644); err != nil {
		t.Fatalf("write .gitkeep: %v", err)
	}
	if _, err := wt.Add(".gitkeep"); err != nil {
		t.Fatalf("wt.Add .gitkeep: %v", err)
	}

	sig := &object.Signature{
		Name:  "Test User",
		Email: "test@example.com",
		When:  time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC),
	}
	if _, err := wt.Commit("chore: initial commit", &git.CommitOptions{
		Author:    sig,
		Committer: sig,
	}); err != nil {
		t.Fatalf("initial commit: %v", err)
	}

	return r
}

// generateFiles creates count text files in dir and returns their relative paths.
func generateFiles(t *testing.T, dir string, count int) []string {
	t.Helper()
	paths := make([]string, count)
	for i := 0; i < count; i++ {
		name := fmt.Sprintf("file_%03d.txt", i)
		content := fmt.Sprintf("// Auto-generated file %d\npackage dummy\n\nconst Value%d = %d\n", i, i, i*i)
		full := filepath.Join(dir, name)
		if err := os.WriteFile(full, []byte(content), 0644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		paths[i] = name
	}
	return paths
}

// buildChunks divides files into groups of groupSize and returns CommitChunk slices.
func buildChunks(files []string, groupSize int) []chunker.CommitChunk {
	var chunks []chunker.CommitChunk
	for i := 0; i < len(files); i += groupSize {
		end := i + groupSize
		if end > len(files) {
			end = len(files)
		}
		group := files[i:end]
		chunks = append(chunks, chunker.CommitChunk{
			CommitMessage: fmt.Sprintf("feat: add files %d-%d", i, end-1),
			FilesInvolved: group,
		})
	}
	return chunks
}

func TestRewriteHistoryIntegration(t *testing.T) {
	const (
		fileCount = 100
		groupSize = 10  // 10 files per commit → 10 commits
		startStr  = "2026-01-01"
		endStr    = "2026-01-30"
	)

	// ── Setup ────────────────────────────────────────────────────────────────
	dir := t.TempDir() // automatically cleaned up
	_ = initRepo(t, dir)

	// ── Generate 100 files ───────────────────────────────────────────────────
	files := generateFiles(t, dir, fileCount)

	// ── Build mock chunks (10 groups of 10 files) ────────────────────────────
	chunks := buildChunks(files, groupSize)
	expectedCommits := len(chunks) // 10

	// ── Execute ──────────────────────────────────────────────────────────────
	if err := rewriter.RewriteHistory(dir, startStr, endStr, chunks); err != nil {
		t.Fatalf("RewriteHistory failed: %v", err)
	}

	// ── Open repo for assertions ─────────────────────────────────────────────
	r, err := git.PlainOpen(dir)
	if err != nil {
		t.Fatalf("git.PlainOpen after rewrite: %v", err)
	}

	ref, err := r.Head()
	if err != nil {
		t.Fatalf("r.Head: %v", err)
	}

	logIter, err := r.Log(&git.LogOptions{From: ref.Hash()})
	if err != nil {
		t.Fatalf("r.Log: %v", err)
	}

	startDate, _ := time.Parse(rewriter.DateLayout, startStr)
	endDate, _ := time.Parse(rewriter.DateLayout, endStr)
	// Add 1 day buffer to endDate to be inclusive of the end day.
	endDateInclusive := endDate.Add(24 * time.Hour)

	var rewrittenCommits []*object.Commit
	err = logIter.ForEach(func(c *object.Commit) error {
		// Skip the initial placeholder commit.
		if c.Message == "chore: initial commit" {
			return nil
		}
		rewrittenCommits = append(rewrittenCommits, c)
		return nil
	})
	if err != nil {
		t.Fatalf("iterating commits: %v", err)
	}

	// ── Assertion 1: commit count ≥ expectedCommits ──────────────────────────
	if len(rewrittenCommits) < expectedCommits {
		t.Errorf("expected at least %d commits, got %d", expectedCommits, len(rewrittenCommits))
	}
	t.Logf("Total rewritten commits: %d (expected %d)", len(rewrittenCommits), expectedCommits)

	// ── Assertion 2: timestamps within date range ────────────────────────────
	for _, c := range rewrittenCommits {
		authorWhen := c.Author.When.UTC()
		committerWhen := c.Committer.When.UTC()

		if authorWhen.Before(startDate) || authorWhen.After(endDateInclusive) {
			t.Errorf("commit %s: author timestamp %v is outside [%v, %v]",
				c.Hash.String()[:8], authorWhen, startDate, endDateInclusive)
		}
		if committerWhen.Before(startDate) || committerWhen.After(endDateInclusive) {
			t.Errorf("commit %s: committer timestamp %v is outside [%v, %v]",
				c.Hash.String()[:8], committerWhen, startDate, endDateInclusive)
		}
		if !authorWhen.Equal(committerWhen) {
			t.Errorf("commit %s: author and committer timestamps differ: %v vs %v",
				c.Hash.String()[:8], authorWhen, committerWhen)
		}
	}

	// ── Assertion 3: non-empty, reasonably formatted commit messages ─────────
	for _, c := range rewrittenCommits {
		msg := strings.TrimSpace(c.Message)
		if msg == "" {
			t.Errorf("commit %s has empty message", c.Hash.String()[:8])
		}
		if len(msg) < 5 {
			t.Errorf("commit %s message too short (%q)", c.Hash.String()[:8], msg)
		}
		// Messages should not be pure whitespace.
		if strings.TrimSpace(msg) == "" {
			t.Errorf("commit %s message is all whitespace", c.Hash.String()[:8])
		}
	}

	// ── Assertion 4: final working tree matches the 100 generated files ──────
	wt, err := r.Worktree()
	if err != nil {
		t.Fatalf("r.Worktree: %v", err)
	}
	status, err := wt.Status()
	if err != nil {
		t.Fatalf("wt.Status: %v", err)
	}
	if !status.IsClean() {
		dirty := []string{}
		for f := range status {
			dirty = append(dirty, f)
		}
		sort.Strings(dirty)
		t.Errorf("working tree is not clean after rewrite; dirty files: %v", dirty)
	}

	// Verify all 100 files exist on disk with correct content.
	for i, relPath := range files {
		full := filepath.Join(dir, relPath)
		data, err := os.ReadFile(full)
		if err != nil {
			t.Errorf("file %s missing after rewrite: %v", relPath, err)
			continue
		}
		expectedContent := fmt.Sprintf("// Auto-generated file %d\npackage dummy\n\nconst Value%d = %d\n", i, i, i*i)
		if string(data) != expectedContent {
			t.Errorf("file %s content mismatch after rewrite", relPath)
		}
	}

	t.Logf("All assertions passed. %d commits with timestamps in [%s, %s].",
		len(rewrittenCommits), startStr, endStr)
}
