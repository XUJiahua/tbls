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
	"github.com/spf13/cobra"
)

var (
	scaffoldOut   string
	scaffoldForce bool
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

		// Generate scaffolded config
		scaffolded, err := GenerateScaffoldConfig(c, s)
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
	Enabled             bool                                 `yaml:"enabled"`
	Include             []string                             `yaml:"include,omitempty"`
	Exclude             []string                             `yaml:"exclude,omitempty"`
	TopN                int                                  `yaml:"topN"`
	SampleSize          int                                  `yaml:"sampleSize"`
	LargeTableThreshold int64                                `yaml:"largeTableThreshold"`
	RecentDays          int                                  `yaml:"recentDays"`
	DateColumn          string                               `yaml:"dateColumn,omitempty"`
	Inference           ScaffoldInference                    `yaml:"inference"`
	Checkpoint          ScaffoldCheckpoint                   `yaml:"checkpoint"`
	Tables              map[string]ScaffoldTableStatsConfig  `yaml:"tables,omitempty"`
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

// ScaffoldTableStatsConfig represents per-table stats settings.
type ScaffoldTableStatsConfig struct {
	DateColumn string `yaml:"dateColumn,omitempty"`
	Skip       bool   `yaml:"skip"`
}

// GenerateScaffoldConfig generates a complete scaffolded config from config and schema.
// This function is exported for use by the serve API.
func GenerateScaffoldConfig(c *config.Config, s *schema.Schema) (*ScaffoldConfig, error) {
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
		Stats:           buildStatsConfig(c, s),
		RequiredVersion: c.RequiredVersion,
	}

	return scaffolded, nil
}

// buildStatsConfig builds ScaffoldStats from config and schema.
func buildStatsConfig(c *config.Config, s *schema.Schema) ScaffoldStats {
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

	// Build inference config
	inference := ScaffoldInference{
		Enabled:                 c.Stats.Inference.Enabled,
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
	tables := make(map[string]ScaffoldTableStatsConfig)
	for _, t := range s.Tables {
		tableCfg := ScaffoldTableStatsConfig{
			Skip: false,
		}
		// Check if there's existing config for this table
		if existing, ok := c.Stats.Tables[t.Name]; ok {
			tableCfg.DateColumn = existing.DateColumn
			tableCfg.Skip = existing.Skip
		}
		tables[t.Name] = tableCfg
	}

	return ScaffoldStats{
		Enabled:             c.Stats.Enabled,
		Include:             c.Stats.Include,
		Exclude:             c.Stats.Exclude,
		TopN:                topN,
		SampleSize:          sampleSize,
		LargeTableThreshold: largeTableThreshold,
		RecentDays:          recentDays,
		DateColumn:          c.Stats.DateColumn,
		Inference:           inference,
		Checkpoint:          checkpoint,
		Tables:              tables,
	}
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
}
