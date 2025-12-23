package rewriter

import (
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"time"

	"git-chronos/internal/identity"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// RewriteCommittedHistory reads all files tracked in repoPath's HEAD, then
// redistributes them into a fresh chronological commit history spanning
// [startDateStr, endDateStr] using the distribution algorithm.
//
// The function replaces the repo's .git directory with the rewritten history.
// The working tree files are untouched.
func RewriteCommittedHistory(repoPath, startDateStr, endDateStr string, idCfg *identity.Config) error {
	// ── 1. Open repo and collect all files from HEAD tree ──────────────────
	r, err := git.PlainOpen(repoPath)
	if err != nil {
		return fmt.Errorf("cannot open repository at %q: %w", repoPath, err)
	}

	ref, err := r.Head()
	if err != nil {
		return fmt.Errorf("cannot get HEAD: %w", err)
	}

	headCommit, err := r.CommitObject(ref.Hash())
	if err != nil {
		return fmt.Errorf("cannot get HEAD commit: %w", err)
	}

	headTree, err := headCommit.Tree()
	if err != nil {
		return fmt.Errorf("cannot get HEAD tree: %w", err)
	}

	// Collect file paths and their contents
	fileContents := map[string][]byte{}
	filePaths := []string{}

	err = headTree.Files().ForEach(func(f *object.File) error {
		contents, readErr := f.Contents()
		if readErr != nil {
			return fmt.Errorf("read %s: %w", f.Name, readErr)
		}
		fileContents[f.Name] = []byte(contents)
		filePaths = append(filePaths, f.Name)
		return nil
	})
	if err != nil {
		return fmt.Errorf("iterating HEAD tree: %w", err)
	}

	if len(filePaths) == 0 {
		return fmt.Errorf("no files found in HEAD tree")
	}

	fmt.Printf("Found %d tracked files in HEAD tree.\n", len(filePaths))

	// ── 2. Calculate distribution ──────────────────────────────────────────
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	plans, err := CalculateDistribution(filePaths, startDateStr, endDateStr, rng)
	if err != nil {
		return fmt.Errorf("calculating distribution: %w", err)
	}

	totalMonths := monthsBetween(mustParseDate(startDateStr), mustParseDate(endDateStr))
	fmt.Printf("Distribution: %d commits across %d months (%s -> %s)\n",
		len(plans), totalMonths, startDateStr, endDateStr)

	// Print per-month summary
	printMonthSummary(plans, startDateStr)

	// ── 3. Build fresh history in a temp directory ─────────────────────────
	tmpDir, err := os.MkdirTemp("", "git-chronos-rewrite-*")
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

	for i, plan := range plans {
		// Write each file in this commit's batch to the temp dir
		for _, relPath := range plan.Files {
			fullPath := filepath.Join(tmpDir, relPath)
			if mkErr := os.MkdirAll(filepath.Dir(fullPath), 0755); mkErr != nil {
				return fmt.Errorf("mkdir for %s: %w", relPath, mkErr)
			}
			if writeErr := os.WriteFile(fullPath, fileContents[relPath], 0644); writeErr != nil {
				return fmt.Errorf("write %s: %w", relPath, writeErr)
			}
			if _, addErr := tmpWt.Add(relPath); addErr != nil {
				return fmt.Errorf("plan %d: stage %s: %w", i, relPath, addErr)
			}
		}

		// Apply timezone from identity config
		loc := idCfg.Location()
		commitTime := plan.Timestamp.In(loc)

		authorSig := &object.Signature{
			Name:  idCfg.Author.Name,
			Email: idCfg.Author.Email,
			When:  commitTime,
		}
		committerSig := &object.Signature{
			Name:  idCfg.Committer.Name,
			Email: idCfg.Committer.Email,
			When:  commitTime,
		}
		hash, commitErr := tmpWt.Commit(plan.Message, &git.CommitOptions{
			Author:    authorSig,
			Committer: committerSig,
		})
		if commitErr != nil {
			return fmt.Errorf("plan %d commit failed: %w", i, commitErr)
		}

		fmt.Printf("  [%d/%d] %s  %s  (%d files)\n",
			i+1, len(plans),
			plan.Timestamp.Format("2006-01-02 15:04"),
			plan.Message,
			len(plan.Files),
		)
		_ = hash
	}

	// ── 4. Verify temp repo working tree is clean ──────────────────────────
	status, err := tmpWt.Status()
	if err != nil {
		return fmt.Errorf("temp repo status check: %w", err)
	}
	if !status.IsClean() {
		return fmt.Errorf("temp repo working tree not clean after rewrite")
	}

	// ── 5. Swap .git directories ───────────────────────────────────────────
	srcGit := filepath.Join(repoPath, ".git")
	backupGit := filepath.Join(repoPath, ".git.bak")
	tmpGit := filepath.Join(tmpDir, ".git")

	// Backup original .git
	if err := os.Rename(srcGit, backupGit); err != nil {
		return fmt.Errorf("backing up .git: %w", err)
	}

	// Copy fresh .git from temp dir
	if err := copyDir(tmpGit, srcGit); err != nil {
		// Restore backup on failure
		os.Rename(backupGit, srcGit)
		return fmt.Errorf("installing new .git: %w", err)
	}

	// Remove backup
	os.RemoveAll(backupGit)

	fmt.Printf("\nHistory rewrite complete. %d commits written.\n", len(plans))
	return nil
}

// mustParseDate panics if date string is invalid — only used internally after validation.
func mustParseDate(s string) time.Time {
	t, err := time.Parse(DateLayout, s)
	if err != nil {
		panic(fmt.Sprintf("mustParseDate(%q): %v", s, err))
	}
	return t
}

// printMonthSummary logs how many commits were assigned per calendar month.
func printMonthSummary(plans []CommitPlan, startDateStr string) {
	counts := map[string]int{}
	for _, p := range plans {
		key := p.Timestamp.Format("2006-01")
		counts[key]++
	}
	fmt.Println("Commits per month:")
	// Print in chronological order
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	// simple sort
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[j] < keys[i] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	for _, k := range keys {
		fmt.Printf("  %s: %d commits\n", k, counts[k])
	}
}

// copyDir recursively copies src directory to dst.
func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)

		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}

		return copyFile(path, target, info.Mode())
	})
}

// copyFile copies a single file from src to dst.
func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}
