package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/k1LoW/tbls/cmdutil"
	"github.com/k1LoW/tbls/config"
	"github.com/k1LoW/tbls/datasource"
	"github.com/k1LoW/tbls/schema"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var (
	scaffoldOut         string
	scaffoldForce       bool
	scaffoldInteractive bool
)

// scaffoldCmd represents the scaffold command.
var scaffoldCmd = &cobra.Command{
	Use:   "scaffold",
	Short: "generate a complete config file with all parameters",
	Long: `'tbls scaffold' generates a complete configuration file with all parameters filled in.
This helps users to:
1. See all available configuration options
2. Fine-tune parameters with minimal input

Examples:
  # Generate config from existing config file
  tbls scaffold -c .tbls.yml

  # Generate config from DSN (no existing config)
  tbls scaffold --dsn "postgres://user:pass@localhost:5432/mydb"

  # Interactive mode - select tables with fuzzy finder
  tbls scaffold --dsn "postgres://..." --interactive
  tbls scaffold -c .tbls.yml -i

  # Specify output file
  tbls scaffold -c .tbls.yml -o my-config.yml

  # Force overwrite without prompt
  tbls scaffold -c .tbls.yml -f
`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if allow, err := cmdutil.IsAllowedToExecute(when); !allow || err != nil {
			if err != nil {
				return err
			}
			return nil
		}

		// Validate input: need either configPath or dsn
		if configPath == "" && dsn == "" {
			return fmt.Errorf("either --config or --dsn is required")
		}

		// Create and load config
		c, err := config.New()
		if err != nil {
			return err
		}

		options := []config.Option{}
		if dsn != "" {
			options = append(options, config.DSNURL(dsn))
		}

		if err := c.Load(configPath, options...); err != nil {
			return err
		}

		// Analyze database schema
		s, err := datasource.Analyze(c.DSN)
		if err != nil {
			return err
		}

		// Interactive mode: let user select tables
		if scaffoldInteractive {
			selectedTables, err := RunTableSelector(s.Tables)
			if err != nil {
				if err.Error() == "cancelled" {
					fmt.Println("Cancelled.")
					return nil
				}
				return err
			}

			if len(selectedTables) == 0 {
				fmt.Print("No tables selected. Continue anyway? [y/N]: ")
				reader := bufio.NewReader(os.Stdin)
				response, err := reader.ReadString('\n')
				if err != nil {
					return err
				}
				response = strings.TrimSpace(strings.ToLower(response))
				if response != "y" && response != "yes" {
					fmt.Println("Aborted.")
					return nil
				}
			}

			// Filter schema to only selected tables
			s = filterSchemaToTables(s, selectedTables)
			// Set include in config
			c.Include = selectedTables
		}

		// Detect date columns for stats configuration
		detectedDateColumns, err := datasource.DetectDateColumns(c.DSN, s)
		if err != nil {
			// Log warning but continue - date column detection is optional
			logrus.WithError(err).Warn("failed to detect date columns")
			detectedDateColumns = make(map[string]string)
		}

		// Generate scaffolded config
		scaffolded, err := GenerateScaffoldConfig(c, s, detectedDateColumns)
		if err != nil {
			return err
		}

		// Determine output path
		outPath := scaffoldOut
		if outPath == "" {
			if configPath != "" {
				outPath = configPath
			} else {
				outPath = ".tbls.yml"
			}
		}

		// Handle file output
		if err := writeScaffoldOutput(outPath, scaffolded, scaffoldForce); err != nil {
			return err
		}

		return nil
	},
}

// ScaffoldConfig is the struct for scaffolded config output.
// It includes all fields with explicit values for user reference.
type ScaffoldConfig struct {
	Name                   string                         `yaml:"name,omitempty"`
	Desc                   string                         `yaml:"desc,omitempty"`
	Labels                 []string                       `yaml:"labels,omitempty"`
	DSN                    config.DSN                     `yaml:"dsn"`
	Include                []string                       `yaml:"include,omitempty"`
	Exclude                []string                       `yaml:"exclude,omitempty"`
	Sort                   bool                           `yaml:"sort"`
	DetectVirtualRelations ScaffoldDetectVirtualRelations `yaml:"detectVirtualRelations"`
	Stats                  ScaffoldStats                  `yaml:"stats"`
	RequiredVersion        string                         `yaml:"requiredVersion,omitempty"`
}

// ScaffoldDetectVirtualRelations represents detectVirtualRelations settings.
type ScaffoldDetectVirtualRelations struct {
	Enabled  bool   `yaml:"enabled"`
	Strategy string `yaml:"strategy,omitempty"`
}

// ScaffoldStats represents stats settings with all options.
type ScaffoldStats struct {
	Enabled             bool                       `yaml:"enabled"`
	Include             []string                   `yaml:"include,omitempty"`
	Exclude             []string                   `yaml:"exclude,omitempty"`
	TopN                int                        `yaml:"topN"`
	SampleSize          int                        `yaml:"sampleSize"`
	LargeTableThreshold int64                      `yaml:"largeTableThreshold"`
	RecentDays          int                        `yaml:"recentDays"`
	UsePgStats          bool                       `yaml:"usePgStats,omitempty"`
	Inference           ScaffoldInference          `yaml:"inference"`
	Checkpoint          ScaffoldCheckpoint         `yaml:"checkpoint"`
	Tables              []ScaffoldTableStatsConfig `yaml:"tables,omitempty"`
}

// ScaffoldInference represents inference settings.
type ScaffoldInference struct {
	Enabled                 bool    `yaml:"enabled"`
	EnumMaxCardinality      float64 `yaml:"enumMaxCardinality"`
	EnumMaxDistinct         int     `yaml:"enumMaxDistinct"`
	DictMaxCardinality      float64 `yaml:"dictMaxCardinality"`
	DictMaxDistinct         int     `yaml:"dictMaxDistinct"`
	ForeignKeyMinConfidence float64 `yaml:"foreignKeyMinConfidence"`
}

// ScaffoldCheckpoint represents checkpoint settings.
type ScaffoldCheckpoint struct {
	Enabled bool   `yaml:"enabled"`
	TTL     string `yaml:"ttl"`
	Force   bool   `yaml:"force"`
}

// ScaffoldTableStatsConfig represents per-table stats settings with explicit sampling mode.
// Mode determines how the table is sampled:
//   - date_filter: query recent data within RecentDays using DateColumn
//   - row_limit: query first SampleSize rows (-1 means no limit)
type ScaffoldTableStatsConfig struct {
	Name       string              `yaml:"name"`
	Mode       config.SamplingMode `yaml:"mode"`
	DateColumn string              `yaml:"dateColumn,omitempty"`
	SampleSize int                 `yaml:"sampleSize,omitempty"`
}

// GenerateScaffoldConfig generates a complete scaffolded config from config and schema.
// This function is exported for use by the serve API.
// detectedDateColumns is an optional map of table name to detected date column name.
func GenerateScaffoldConfig(c *config.Config, s *schema.Schema, detectedDateColumns map[string]string) (*ScaffoldConfig, error) {
	scaffolded := &ScaffoldConfig{
		Name:    c.Name,
		Desc:    c.Desc,
		Labels:  c.Labels,
		DSN:     c.DSN,
		Include: c.Include,
		Exclude: c.Exclude,
		Sort:    c.Sort,
		DetectVirtualRelations: ScaffoldDetectVirtualRelations{
			Enabled:  c.DetectVirtualRelations.Enabled,
			Strategy: c.DetectVirtualRelations.Strategy,
		},
		Stats:           buildStatsConfig(c, s, detectedDateColumns),
		RequiredVersion: c.RequiredVersion,
	}

	return scaffolded, nil
}

// buildStatsConfig builds ScaffoldStats from config and schema.
// detectedDateColumns is an optional map of table name to auto-detected date column name.
func buildStatsConfig(c *config.Config, s *schema.Schema, detectedDateColumns map[string]string) ScaffoldStats {
	// Get default values
	defaultInference := config.DefaultInferenceConfig()
	defaultCheckpoint := config.DefaultCheckpointConfig()

	// Use config values if set, otherwise use defaults
	topN := c.Stats.TopN
	if topN == 0 {
		topN = 10
	}
	sampleSize := c.Stats.SampleSize
	if sampleSize == 0 {
		sampleSize = 10000
	}
	largeTableThreshold := c.Stats.LargeTableThreshold
	if largeTableThreshold == 0 {
		largeTableThreshold = 1000000
	}
	recentDays := c.Stats.RecentDays
	if recentDays == 0 {
		recentDays = 30
	}

	// Build inference config (default enabled to true for scaffold)
	inference := ScaffoldInference{
		Enabled:                 true,
		EnumMaxCardinality:      c.Stats.Inference.EnumMaxCardinality,
		EnumMaxDistinct:         c.Stats.Inference.EnumMaxDistinct,
		DictMaxCardinality:      c.Stats.Inference.DictMaxCardinality,
		DictMaxDistinct:         c.Stats.Inference.DictMaxDistinct,
		ForeignKeyMinConfidence: c.Stats.Inference.ForeignKeyMinConfidence,
	}
	// Apply defaults if not set
	if inference.EnumMaxCardinality == 0 {
		inference.EnumMaxCardinality = defaultInference.EnumMaxCardinality
	}
	if inference.EnumMaxDistinct == 0 {
		inference.EnumMaxDistinct = defaultInference.EnumMaxDistinct
	}
	if inference.DictMaxCardinality == 0 {
		inference.DictMaxCardinality = defaultInference.DictMaxCardinality
	}
	if inference.DictMaxDistinct == 0 {
		inference.DictMaxDistinct = defaultInference.DictMaxDistinct
	}
	if inference.ForeignKeyMinConfidence == 0 {
		inference.ForeignKeyMinConfidence = defaultInference.ForeignKeyMinConfidence
	}

	// Build checkpoint config
	checkpointTTL := c.Stats.Checkpoint.TTL
	if checkpointTTL == "" {
		checkpointTTL = defaultCheckpoint.TTL
	}
	checkpoint := ScaffoldCheckpoint{
		Enabled: c.Stats.Checkpoint.Enabled,
		TTL:     checkpointTTL,
		Force:   c.Stats.Checkpoint.Force,
	}

	// Build per-table stats config from schema
	// Priority: user config > auto-detected value > global default
	var tables []ScaffoldTableStatsConfig
	for _, t := range s.Tables {
		tableCfg := ScaffoldTableStatsConfig{
			Name: t.Name,
		}

		// Check user config first (highest priority)
		userConfig := findUserTableConfig(c.Stats.Tables, t.Name)
		if userConfig != nil && userConfig.Mode != "" {
			// User explicitly set mode
			tableCfg.Mode = config.SamplingMode(userConfig.Mode)
			tableCfg.DateColumn = userConfig.DateColumn
			tableCfg.SampleSize = userConfig.SampleSize
		} else {
			// Auto-detect: try date column first, fall back to row limit
			var detectedDateCol string
			if detectedDateColumns != nil {
				detectedDateCol = detectedDateColumns[t.Name]
			}
			// User can override detected date column
			if userConfig != nil && userConfig.DateColumn != "" {
				detectedDateCol = userConfig.DateColumn
			}

			if detectedDateCol != "" {
				// Use date filter mode
				tableCfg.Mode = config.SamplingModeDateFilter
				tableCfg.DateColumn = detectedDateCol
			} else {
				// Fall back to row limit mode with global sampleSize
				tableCfg.Mode = config.SamplingModeRowLimit
				tableCfg.SampleSize = sampleSize
			}
		}

		tables = append(tables, tableCfg)
	}

	return ScaffoldStats{
		Enabled:             true, // Default to true for scaffold
		Include:             c.Stats.Include,
		Exclude:             c.Stats.Exclude,
		TopN:                topN,
		SampleSize:          sampleSize,
		LargeTableThreshold: largeTableThreshold,
		RecentDays:          recentDays,
		UsePgStats:          c.Stats.UsePgStats,
		Inference:           inference,
		Checkpoint:          checkpoint,
		Tables:              tables,
	}
}

// findUserTableConfig searches for a table config by name in the list
func findUserTableConfig(tables []config.TableStatsConfig, tableName string) *config.TableStatsConfig {
	for i := range tables {
		if tables[i].Name == tableName {
			return &tables[i]
		}
	}
	return nil
}

// writeScaffoldOutput writes the scaffolded config to file with overwrite prompt.
func writeScaffoldOutput(outPath string, scaffolded *ScaffoldConfig, forceOverwrite bool) error {
	absPath, err := filepath.Abs(outPath)
	if err != nil {
		return err
	}

	// Check if file exists
	if _, err := os.Stat(absPath); err == nil {
		if !forceOverwrite {
			// File exists, prompt user
			fmt.Printf("File '%s' already exists.\n", absPath)
			fmt.Print("Overwrite? [y/N/s(save as .tbls.scaffold.yml)]: ")

			reader := bufio.NewReader(os.Stdin)
			response, err := reader.ReadString('\n')
			if err != nil {
				return err
			}
			response = strings.TrimSpace(strings.ToLower(response))

			switch response {
			case "y", "yes":
				// Continue with overwrite
			case "s", "scaffold":
				// Save as scaffold file
				dir := filepath.Dir(absPath)
				absPath = filepath.Join(dir, ".tbls.scaffold.yml")
				fmt.Printf("Saving to '%s'\n", absPath)
			default:
				fmt.Println("Aborted.")
				return nil
			}
		}
	}

	// Create parent directories if needed
	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	// Marshal to YAML
	data, err := yaml.Marshal(scaffolded)
	if err != nil {
		return err
	}

	// Add header comment
	header := "# Generated by tbls scaffold\n# This config file contains all available parameters with their current/default values.\n# Modify as needed and remove this comment.\n\n"

	content := header + string(data)

	// Write file
	if err := os.WriteFile(absPath, []byte(content), 0644); err != nil {
		return err
	}

	fmt.Printf("Config file written to '%s'\n", absPath)
	return nil
}

func init() {
	rootCmd.AddCommand(scaffoldCmd)
	scaffoldCmd.Flags().StringVarP(&configPath, "config", "c", "", "config file path")
	scaffoldCmd.Flags().StringVarP(&dsn, "dsn", "", "", "data source name (required if --config is not specified)")
	scaffoldCmd.Flags().StringVarP(&scaffoldOut, "out", "o", "", "output file path (default: overwrite config file or .tbls.yml)")
	scaffoldCmd.Flags().BoolVarP(&scaffoldForce, "force", "f", false, "force overwrite without prompt")
	scaffoldCmd.Flags().BoolVarP(&scaffoldInteractive, "interactive", "i", false, "interactive mode - select tables with fuzzy finder")
}
