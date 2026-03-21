package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"git-chronos/internal/identity"
	"git-chronos/internal/rewriter"
)

var (
	scrambleStartDate string
	scrambleEndDate   string
	scrambleRepoPath  string
)

var scrambleCmd = &cobra.Command{
	Use:   "scramble",
	Short: "Spread existing commit dates across a date range, preserving diffs and messages",
	Long: `scramble rewrites commit timestamps in the existing history, keeping all diffs
and commit messages intact. The number of commits stays the same; only their
dates are redistributed across [--start-date, --end-date] using the same
per-month distribution algorithm as the rewrite command.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("git-chronos scramble\n  repo:       %s\n  start-date: %s\n  end-date:   %s\n",
			scrambleRepoPath, scrambleStartDate, scrambleEndDate)

		if scrambleStartDate == "" || scrambleEndDate == "" {
			fmt.Fprintln(os.Stderr, "Error: --start-date and --end-date are required (format: YYYY-MM-DD)")
			os.Exit(1)
		}

		idCfg, err := identity.LoadFromRepo(scrambleRepoPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "\n%v\n", err)
			os.Exit(1)
		}
		fmt.Printf("  identity:   %s/%s (author: %s <%s>)\n\n",
			scrambleRepoPath, identity.IdentityFileName, idCfg.Author.Name, idCfg.Author.Email)

		if err := rewriter.ScrambleCommitDates(scrambleRepoPath, scrambleStartDate, scrambleEndDate, idCfg); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	},
}

func init() {
	scrambleCmd.Flags().StringVar(&scrambleStartDate, "start-date", "", "Start date for the new date range (e.g. 2024-01-01)")
	scrambleCmd.Flags().StringVar(&scrambleEndDate, "end-date", "", "End date for the new date range (e.g. 2024-12-31)")
	scrambleCmd.Flags().StringVar(&scrambleRepoPath, "repo-path", ".", "Path to the git repository")

	rootCmd.AddCommand(scrambleCmd)
}
