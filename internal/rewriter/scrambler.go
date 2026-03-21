package rewriter

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"

	"git-chronos/internal/identity"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// ScrambleCommitDates replays the existing commit history with new timestamps
// spread across [startDateStr, endDateStr], preserving all diffs and messages.
func ScrambleCommitDates(repoPath, startDateStr, endDateStr string, idCfg *identity.Config) error {
	// ── 1. Open repo and collect commits in chronological order ────────────
	r, err := git.PlainOpen(repoPath)
	if err != nil {
		return fmt.Errorf("cannot open repository at %q: %w", repoPath, err)
	}

	ref, err := r.Head()
	if err != nil {
		return fmt.Errorf("cannot get HEAD: %w", err)
	}

	logIter, err := r.Log(&git.LogOptions{From: ref.Hash()})
	if err != nil {
		return fmt.Errorf("cannot get log: %w", err)
	}

	var commits []*object.Commit
	if err := logIter.ForEach(func(c *object.Commit) error {
		commits = append(commits, c)
		return nil
	}); err != nil {
		return fmt.Errorf("iterating commits: %w", err)
	}

	if len(commits) == 0 {
		return fmt.Errorf("no commits found")
	}

	// Reverse: log returns newest-first, we need oldest-first
	for i, j := 0, len(commits)-1; i < j; i, j = i+1, j-1 {
		commits[i], commits[j] = commits[j], commits[i]
	}

	fmt.Printf("Found %d commits in history.\n", len(commits))

	// ── 2. Generate new timestamps ──────────────────────────────────────────
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	timestamps, err := GenerateTimestamps(len(commits), startDateStr, endDateStr, rng)
	if err != nil {
		return fmt.Errorf("generating timestamps: %w", err)
	}

	totalMonths := monthsBetween(mustParseDate(startDateStr), mustParseDate(endDateStr))
	fmt.Printf("Scrambling: %d commits across %d months (%s -> %s)\n",
		len(commits), totalMonths, startDateStr, endDateStr)

	// Print per-month summary using generated timestamps
	type fakePlan struct{ Timestamp time.Time }
	plans := make([]CommitPlan, len(timestamps))
	for i, ts := range timestamps {
		plans[i] = CommitPlan{Timestamp: ts}
	}
	printMonthSummary(plans, startDateStr)

	// ── 3. Build fresh history in a temp directory ──────────────────────────
	tmpDir, err := os.MkdirTemp("", "git-chronos-scramble-*")
	if err != nil {
		return fmt.Errorf("creating temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	tmpRepo, err := git.PlainInit(tmpDir, false)
	if err != nil {
		return fmt.Errorf("init temp repo: %w", err)
	}

	tmpWt, err := tmpRepo.Worktree()
	if err != nil {
		return fmt.Errorf("temp worktree: %w", err)
	}

	loc := idCfg.Location()

	// Track the previous commit's tree state (file path -> blob hash)
	prevFiles := map[string]plumbing.Hash{}

	for i, commit := range commits {
		newTime := timestamps[i].In(loc)

		// Get this commit's tree
		tree, err := commit.Tree()
		if err != nil {
			return fmt.Errorf("commit %d: get tree: %w", i, err)
		}

		// Build new file state
		newFiles := map[string]plumbing.Hash{}
		if err := tree.Files().ForEach(func(f *object.File) error {
			newFiles[f.Name] = f.Hash
			return nil
		}); err != nil {
			return fmt.Errorf("commit %d: iterate tree: %w", i, err)
		}

		// Determine changes vs previous state
		var changed []string
		var deleted []string
		for name, hash := range newFiles {
			if oldHash, exists := prevFiles[name]; !exists || oldHash != hash {
				changed = append(changed, name)
			}
		}
		for name := range prevFiles {
			if _, exists := newFiles[name]; !exists {
				deleted = append(deleted, name)
			}
		}

		// Apply changes to temp working directory
		for _, name := range changed {
			f, err := tree.File(name)
			if err != nil {
				return fmt.Errorf("commit %d: get file %s: %w", i, name, err)
			}
			content, err := f.Contents()
			if err != nil {
				return fmt.Errorf("commit %d: read file %s: %w", i, name, err)
			}
			fullPath := filepath.Join(tmpDir, name)
			if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
				return fmt.Errorf("commit %d: mkdir for %s: %w", i, name, err)
			}
			if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
				return fmt.Errorf("commit %d: write %s: %w", i, name, err)
			}
			if _, err := tmpWt.Add(name); err != nil {
				return fmt.Errorf("commit %d: stage %s: %w", i, name, err)
			}
		}
		for _, name := range deleted {
			fullPath := filepath.Join(tmpDir, name)
			if err := os.Remove(fullPath); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("commit %d: delete %s: %w", i, name, err)
			}
			if _, err := tmpWt.Remove(name); err != nil {
				return fmt.Errorf("commit %d: unstage %s: %w", i, name, err)
			}
		}

		authorSig := &object.Signature{
			Name:  idCfg.Author.Name,
			Email: idCfg.Author.Email,
			When:  newTime,
		}
		committerSig := &object.Signature{
			Name:  idCfg.Committer.Name,
			Email: idCfg.Committer.Email,
			When:  newTime,
		}

		hash, err := tmpWt.Commit(commit.Message, &git.CommitOptions{
			Author:    authorSig,
			Committer: committerSig,
		})
		if err != nil {
			return fmt.Errorf("commit %d failed: %w", i, err)
		}
		_ = hash

		fmt.Printf("  [%d/%d] %s  %s\n",
			i+1, len(commits),
			newTime.Format("2006-01-02 15:04"),
			truncateMsg(commit.Message, 60),
		)

		prevFiles = newFiles
	}

	// ── 4. Swap .git directories ────────────────────────────────────────────
	srcGit := filepath.Join(repoPath, ".git")
	backupGit := filepath.Join(repoPath, ".git.bak")
	tmpGit := filepath.Join(tmpDir, ".git")

	if err := os.Rename(srcGit, backupGit); err != nil {
		return fmt.Errorf("backing up .git: %w", err)
	}
	if err := copyDir(tmpGit, srcGit); err != nil {
		os.Rename(backupGit, srcGit)
		return fmt.Errorf("installing new .git: %w", err)
	}
	os.RemoveAll(backupGit)

	fmt.Printf("\nCommit date scramble complete. %d commits rewritten.\n", len(commits))
	return nil
}

// truncateMsg trims trailing newlines and caps the string at max runes.
func truncateMsg(s string, max int) string {
	s = strings.TrimRight(s, "\n\r")
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
