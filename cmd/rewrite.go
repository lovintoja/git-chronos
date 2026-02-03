package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"git-chronos/internal/identity"
	"git-chronos/internal/rewriter"
)

var (
	startDate string
	endDate   string
	repoPath  string
)

var rewriteCmd = &cobra.Command{
	Use:   "rewrite",
	Short: "Rewrite git commit timestamps within a date range",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("git-chronos rewrite\n  repo:       %s\n  start-date: %s\n  end-date:   %s\n",
			repoPath, startDate, endDate)

		if startDate == "" || endDate == "" {
			fmt.Fprintln(os.Stderr, "Error: --start-date and --end-date are required (format: YYYY-MM-DD)")
			os.Exit(1)
		}

		idCfg, err := identity.LoadFromRepo(repoPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "\n%v\n", err)
			os.Exit(1)
		}
		fmt.Printf("  identity:   %s/%s (author: %s <%s>)\n\n",
			repoPath, identity.IdentityFileName, idCfg.Author.Name, idCfg.Author.Email)

		if err := rewriter.RewriteCommittedHistory(repoPath, startDate, endDate, idCfg); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	},
}

func init() {
	rewriteCmd.Flags().StringVar(&startDate, "start-date", "", "Start date for rewriting (e.g. 2024-01-01)")
	rewriteCmd.Flags().StringVar(&endDate, "end-date", "", "End date for rewriting (e.g. 2024-12-31)")
	rewriteCmd.Flags().StringVar(&repoPath, "repo-path", ".", "Path to the git repository")

	rootCmd.AddCommand(rewriteCmd)
}
