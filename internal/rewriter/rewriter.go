package rewriter

import (
	"fmt"
	"time"

	"git-chronos/internal/chunker"
	"git-chronos/internal/identity"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// DateLayout is the expected format for --start-date / --end-date flags.
const DateLayout = "2006-01-02"

// RewriteHistory stages and commits each chunk with an interpolated timestamp.
// startDateStr and endDateStr must be in "YYYY-MM-DD" format.
func RewriteHistory(repoPath, startDateStr, endDateStr string, chunks []chunker.CommitChunk) error {
	if len(chunks) == 0 {
		return fmt.Errorf("no commit chunks provided")
	}

	startDate, err := time.Parse(DateLayout, startDateStr)
	if err != nil {
		return fmt.Errorf("invalid --start-date %q (expected YYYY-MM-DD): %w", startDateStr, err)
	}
	endDate, err := time.Parse(DateLayout, endDateStr)
	if err != nil {
		return fmt.Errorf("invalid --end-date %q (expected YYYY-MM-DD): %w", endDateStr, err)
	}
	if !endDate.After(startDate) {
		return fmt.Errorf("--end-date must be after --start-date")
	}

	r, err := git.PlainOpen(repoPath)
	if err != nil {
		return fmt.Errorf("cannot open repository at %q: %w", repoPath, err)
	}

	wt, err := r.Worktree()
	if err != nil {
		return fmt.Errorf("cannot get worktree: %w", err)
	}

	totalDuration := endDate.Sub(startDate)
	n := len(chunks)

	for i, chunk := range chunks {
		// Linear interpolation: chunk 0 → startDate, chunk n-1 → endDate
		var commitTime time.Time
		if n == 1 {
			commitTime = startDate
		} else {
			fraction := float64(i) / float64(n-1)
			commitTime = startDate.Add(time.Duration(float64(totalDuration) * fraction))
		}

		// Stage only the files involved in this chunk
		if len(chunk.FilesInvolved) == 0 {
			return fmt.Errorf("chunk %d (%q) has no files_involved", i, chunk.CommitMessage)
		}
		for _, filePath := range chunk.FilesInvolved {
			if _, addErr := wt.Add(filePath); addErr != nil {
				return fmt.Errorf("chunk %d: failed to stage %q: %w", i, filePath, addErr)
			}
		}

		// Build commit options with forged timestamps using default identity
		def := identity.DefaultConfig()
		authorSig := &object.Signature{Name: def.Author.Name, Email: def.Author.Email, When: commitTime}
		committerSig := &object.Signature{Name: def.Committer.Name, Email: def.Committer.Email, When: commitTime}
		commitOpts := &git.CommitOptions{
			Author:    authorSig,
			Committer: committerSig,
		}

		hash, err := wt.Commit(chunk.CommitMessage, commitOpts)
		if err != nil {
			return fmt.Errorf("chunk %d: commit failed: %w", i, err)
		}

		fmt.Printf("  [%d/%d] %s committed at %s (hash: %s)\n",
			i+1, n,
			chunk.CommitMessage,
			commitTime.Format("2006-01-02 15:04:05"),
			hash.String()[:8],
		)
	}

	// Verify working tree is clean after all commits
	status, err := wt.Status()
	if err != nil {
		return fmt.Errorf("could not verify final working tree status: %w", err)
	}
	if !status.IsClean() {
		unstaged := []string{}
		for f, s := range status {
			if s.Worktree != git.Unmodified || s.Staging != git.Unmodified {
				unstaged = append(unstaged, f)
			}
		}
		return fmt.Errorf("working tree is not clean after rewrite; unstaged files: %v", unstaged)
	}

	fmt.Println("Working tree is clean. Rewrite complete.")
	return nil
}
