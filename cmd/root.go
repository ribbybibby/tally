package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"cloud.google.com/go/bigquery"
	"github.com/ribbybibby/tally/internal/scorecard"
	"github.com/ribbybibby/tally/internal/tally"
	"github.com/spf13/cobra"
)

type rootOptions struct {
	All            bool
	Format         string
	GenerateScores bool
	Output         string
	ProjectID      string
	Table          string
}

var ro rootOptions

var rootCmd = &cobra.Command{
	Use:   "tally",
	Short: "Finds OpenSSF Scorecard scores for packages in a Software Bill of Materials.",
	Long:  `Finds OpenSSF Scorecard scores for packages in a Software Bill of Materials.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) (err error) {
		if ro.GenerateScores && os.Getenv("GITHUB_TOKEN") == "" {
			return fmt.Errorf("must set GITHUB_TOKEN environment variable with -g/--generate")
		}

		if ro.GenerateScores && ro.Table == "" {
			return fmt.Errorf("-t/--table must be set with -g/--generate")
		}

		ctx := context.Background()

		out, err := tally.NewOutput(
			tally.WithOutputFormat(tally.OutputFormat(ro.Output)),
			tally.WithOutputAll(ro.All),
		)
		if err != nil {
			return err
		}

		bq, err := bigquery.NewClient(ctx, ro.ProjectID)
		if err != nil {
			return err
		}

		var table scorecard.Table
		if ro.Table != "" {
			table, err = scorecard.NewTable(bq, ro.Table)
			if err != nil {
				return err
			}
		}

		var r io.ReadCloser
		if args[0] == "-" {
			r = os.Stdin
		} else {
			r, err = os.Open(args[0])
			if err != nil {
				return err
			}
			defer r.Close()
		}

		var results []tally.Result

		// Get packages from BOM
		pkgs, err := tally.PackagesFromBOM(r, tally.BOMFormat(ro.Format))
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "Found %d supported packages in BOM\n", len(pkgs))

		// Add the packages to the results
		for i, pkg := range pkgs {
			// We can infer the github repository from the Go module
			// path
			var repo string
			if pkg.Type == tally.PackageTypeGo && strings.HasPrefix(pkg.Name, "github.com/") {
				c := strings.Split(pkg.Name, "/")
				if len(c) >= 3 {
					repo = strings.Join([]string{c[0], c[1], c[2]}, "/")
				}
			}
			results = append(results, tally.Result{Package: &pkgs[i], Repository: repo})
		}

		// Get repositories from deps.dev
		fmt.Fprintf(os.Stderr, "Fetching repository information from deps.dev dataset...\n")
		results, err = tally.AddRepositoriesFromDepsDev(ctx, bq, results)
		if err != nil {
			return err
		}

		// Integrate scores from the OpenSSF scorecard dataset
		fmt.Fprintf(os.Stderr, "Fetching scores from the latest OpenSSF scorecard dataset...\n")
		results, err = tally.AddScoresFromScorecardLatest(ctx, bq, results)
		if err != nil {
			return err
		}

		// Integrate scores from a private scorecard dataset
		if table != nil {
			fmt.Fprintf(os.Stderr, "Fetching scores from %s...\n", table)
			results, err = tally.AddScoresFromScorecardTable(ctx, table, results)
			if err != nil {
				return err
			}
		}

		// Generate missing scores
		if ro.GenerateScores && table != nil {
			fmt.Fprintf(os.Stderr, "Generating missing scores...\n")
			results, err = tally.GenerateScores(ctx, table, results)
			if err != nil {
				return err
			}
		}

		return out.WriteResults(os.Stdout, results)
	},
}

func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.Flags().StringVarP(&ro.ProjectID, "project-id", "p", "", "Google Cloud project that Big Query requests are billed against")
	rootCmd.MarkFlagRequired("project-id")
	rootCmd.Flags().StringVarP(&ro.Format, "format", "f", string(tally.BOMFormatCycloneDXJSON), fmt.Sprintf("BOM format, options=%s", tally.BOMFormats))
	rootCmd.Flags().BoolVarP(&ro.All, "all", "a", false, "print all packages, even those without a scorecard score")
	rootCmd.Flags().StringVarP(&ro.Output, "output", "o", "short", fmt.Sprintf("output format, options=%s", tally.OutputFormats))
	rootCmd.Flags().BoolVarP(&ro.GenerateScores, "generate", "g", false, "generate scores for repositories that aren't in the public dataset. The GITHUB_TOKEN environment variable must be set.")
	rootCmd.Flags().StringVarP(&ro.Table, "table", "t", "", "additional scorecard table for generated scores")
}
