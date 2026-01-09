package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/k1LoW/tbls/datasource"
	"github.com/spf13/cobra"
)

// exploreCmd represents the explore command.
var exploreCmd = &cobra.Command{
	Use:   "explore [JSON_FILE]",
	Short: "Interactively explore database schema and statistics",
	Long: `Interactively explore database schema and statistics in a TUI.

The TUI provides:
- Tree navigation through databases, tables, and columns
- Data quality metrics (null rate, cardinality, distribution)
- Search functionality for tables and columns
- ASCII chart visualization for value distribution

Examples:
  tbls explore metadata.json
  tbls explore json://path/to/schema.json
`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		jsonPath := args[0]

		// Handle both direct path and json:// prefix
		if !strings.HasPrefix(jsonPath, "json://") && !strings.HasPrefix(jsonPath, "{") {
			// Check if file exists
			if _, err := os.Stat(jsonPath); os.IsNotExist(err) {
				return fmt.Errorf("file not found: %s", jsonPath)
			}
		}

		// Load schema from JSON
		s, err := datasource.AnalyzeJSONStringOrFile(jsonPath)
		if err != nil {
			return fmt.Errorf("failed to load schema: %w", err)
		}

		// Run the TUI explorer
		return RunExplorer(s)
	},
}

func init() {
	rootCmd.AddCommand(exploreCmd)
}
