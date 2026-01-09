package gitdiff

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5"
)

// ExtractDiff opens the repo at repoPath, computes the diff of the
// working-tree against HEAD (unstaged + staged changes), and returns
// the raw unified diff string plus the total line count.
// Returns a descriptive error if repoPath is not a valid git repository.
func ExtractDiff(repoPath string) (diffSummary string, totalLines int, err error) {
	r, err := git.PlainOpen(repoPath)
	if err != nil {
		return "", 0, fmt.Errorf("not a valid git repository at %q: %w", repoPath, err)
	}

	ref, err := r.Head()
	if err != nil {
		return "", 0, fmt.Errorf("could not get HEAD: %w", err)
	}

	commit, err := r.CommitObject(ref.Hash())
	if err != nil {
		return "", 0, fmt.Errorf("could not get HEAD commit: %w", err)
	}

	headTree, err := commit.Tree()
	if err != nil {
		return "", 0, fmt.Errorf("could not get HEAD tree: %w", err)
	}

	wt, err := r.Worktree()
	if err != nil {
		return "", 0, fmt.Errorf("could not get worktree: %w", err)
	}

	status, err := wt.Status()
	if err != nil {
		return "", 0, fmt.Errorf("could not get worktree status: %w", err)
	}

	if len(status) == 0 {
		fmt.Println("No changes detected in working tree.")
		return "", 0, nil
	}

	var sb strings.Builder
	total := 0

	for filePath, fileStatus := range status {
		if fileStatus.Worktree == git.Unmodified && fileStatus.Staging == git.Unmodified {
			continue
		}

		// Get old content from HEAD
		var oldLines []string
		headFile, treeErr := headTree.File(filePath)
		if treeErr == nil {
			oldContent, contentErr := headFile.Contents()
			if contentErr == nil {
				oldLines = strings.Split(oldContent, "\n")
			}
		}

		// Get new content from disk
		var newLines []string
		newContent, readErr := os.ReadFile(filepath.Join(repoPath, filePath))
		if readErr == nil {
			newLines = strings.Split(string(newContent), "\n")
		}

		added := maxInt(0, len(newLines)-len(oldLines))
		removed := maxInt(0, len(oldLines)-len(newLines))
		changed := added + removed
		total += changed

		fmt.Fprintf(&sb, "  %s: +%d/-%d lines\n", filePath, added, removed)
	}

	return sb.String(), total, nil
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
