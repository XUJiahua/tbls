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
	scaffoldOut string
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
		if err := writeScaffoldOutput(outPath, scaffolded, force); err != nil {
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
	DocPath                string                         `yaml:"docPath"`
	Format                 ScaffoldFormat                 `yaml:"format"`
	ER                     ScaffoldER                     `yaml:"er"`
	Include                []string                       `yaml:"include,omitempty"`
	Exclude                []string                       `yaml:"exclude,omitempty"`
	Lint                   ScaffoldLint                   `yaml:"lint"`
	LintExclude            []string                       `yaml:"lintExclude,omitempty"`
	Relations              []config.AdditionalRelation    `yaml:"relations,omitempty"`
	Comments               []config.AdditionalComment     `yaml:"comments,omitempty"`
	DetectVirtualRelations ScaffoldDetectVirtualRelations `yaml:"detectVirtualRelations"`
	Stats                  ScaffoldStats                  `yaml:"stats"`
	BaseURL                string                         `yaml:"baseUrl,omitempty"`
	RequiredVersion        string                         `yaml:"requiredVersion,omitempty"`
	DisableOutputSchema    bool                           `yaml:"disableOutputSchema"`
	// Viewpoints is commented out as an example
	// viewpoints: []
}

// ScaffoldFormat represents format settings with explicit defaults.
type ScaffoldFormat struct {
	Adjust                   bool     `yaml:"adjust"`
	Sort                     bool     `yaml:"sort"`
	Number                   bool     `yaml:"number"`
	ShowOnlyFirstParagraph   bool     `yaml:"showOnlyFirstParagraph"`
	HideColumnsWithoutValues []string `yaml:"hideColumnsWithoutValues,omitempty"`
}

// ScaffoldER represents ER settings with explicit defaults.
type ScaffoldER struct {
	Skip            bool                    `yaml:"skip"`
	Format          string                  `yaml:"format"`
	Comment         bool                    `yaml:"comment"`
	HideDef         bool                    `yaml:"hideDef"`
	ShowColumnTypes *config.ShowColumnTypes `yaml:"showColumnTypes,omitempty"`
	Distance        int                     `yaml:"distance"`
	Font            string                  `yaml:"font,omitempty"`
}

// ScaffoldDetectVirtualRelations represents detectVirtualRelations settings.
type ScaffoldDetectVirtualRelations struct {
	Enabled  bool   `yaml:"enabled"`
	Strategy string `yaml:"strategy,omitempty"`
}

// ScaffoldLint represents lint settings with all rules.
type ScaffoldLint struct {
	RequireTableComment      ScaffoldRequireTableComment      `yaml:"requireTableComment"`
	RequireColumnComment     ScaffoldRequireColumnComment     `yaml:"requireColumnComment"`
	RequireIndexComment      ScaffoldRequireIndexComment      `yaml:"requireIndexComment"`
	RequireConstraintComment ScaffoldRequireConstraintComment `yaml:"requireConstraintComment"`
	RequireTriggerComment    ScaffoldRequireTriggerComment    `yaml:"requireTriggerComment"`
	RequireTableLabels       ScaffoldRequireTableLabels       `yaml:"requireTableLabels"`
	UnrelatedTable           ScaffoldUnrelatedTable           `yaml:"unrelatedTable"`
	ColumnCount              ScaffoldColumnCount              `yaml:"columnCount"`
	RequireColumns           ScaffoldRequireColumns           `yaml:"requireColumns"`
	DuplicateRelations       ScaffoldDuplicateRelations       `yaml:"duplicateRelations"`
	RequireForeignKeyIndex   ScaffoldRequireForeignKeyIndex   `yaml:"requireForeignKeyIndex"`
	LabelStyleBigQuery       ScaffoldLabelStyleBigQuery       `yaml:"labelStyleBigQuery"`
	RequireViewpoints        ScaffoldRequireViewpoints        `yaml:"requireViewpoints"`
}

type ScaffoldRequireTableComment struct {
	Enabled      bool     `yaml:"enabled"`
	AllOrNothing bool     `yaml:"allOrNothing"`
	Exclude      []string `yaml:"exclude,omitempty"`
}

type ScaffoldRequireColumnComment struct {
	Enabled       bool     `yaml:"enabled"`
	AllOrNothing  bool     `yaml:"allOrNothing"`
	Exclude       []string `yaml:"exclude,omitempty"`
	ExcludeTables []string `yaml:"excludeTables,omitempty"`
}

type ScaffoldRequireIndexComment struct {
	Enabled       bool     `yaml:"enabled"`
	AllOrNothing  bool     `yaml:"allOrNothing"`
	Exclude       []string `yaml:"exclude,omitempty"`
	ExcludeTables []string `yaml:"excludeTables,omitempty"`
}

type ScaffoldRequireConstraintComment struct {
	Enabled       bool     `yaml:"enabled"`
	AllOrNothing  bool     `yaml:"allOrNothing"`
	Exclude       []string `yaml:"exclude,omitempty"`
	ExcludeTables []string `yaml:"excludeTables,omitempty"`
}

type ScaffoldRequireTriggerComment struct {
	Enabled       bool     `yaml:"enabled"`
	AllOrNothing  bool     `yaml:"allOrNothing"`
	Exclude       []string `yaml:"exclude,omitempty"`
	ExcludeTables []string `yaml:"excludeTables,omitempty"`
}

type ScaffoldRequireTableLabels struct {
	Enabled      bool     `yaml:"enabled"`
	AllOrNothing bool     `yaml:"allOrNothing"`
	Exclude      []string `yaml:"exclude,omitempty"`
}

type ScaffoldRequireViewpoints struct {
	Enabled bool     `yaml:"enabled"`
	Exclude []string `yaml:"exclude,omitempty"`
}

type ScaffoldUnrelatedTable struct {
	Enabled      bool     `yaml:"enabled"`
	AllOrNothing bool     `yaml:"allOrNothing"`
	Exclude      []string `yaml:"exclude,omitempty"`
}

type ScaffoldColumnCount struct {
	Enabled bool     `yaml:"enabled"`
	Max     int      `yaml:"max"`
	Exclude []string `yaml:"exclude,omitempty"`
}

type ScaffoldRequireColumns struct {
	Enabled bool                          `yaml:"enabled"`
	Columns []config.RequireColumnsColumn `yaml:"columns,omitempty"`
}

type ScaffoldDuplicateRelations struct {
	Enabled bool `yaml:"enabled"`
}

type ScaffoldRequireForeignKeyIndex struct {
	Enabled bool     `yaml:"enabled"`
	Exclude []string `yaml:"exclude,omitempty"`
}

type ScaffoldLabelStyleBigQuery struct {
	Enabled bool     `yaml:"enabled"`
	Exclude []string `yaml:"exclude,omitempty"`
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
	// Build comments from schema
	comments := buildCommentsFromSchema(s, c.Comments)

	// Build relations (existing + inferred)
	relations := buildRelationsFromSchema(s, c.Relations)

	// Get ER distance (default is 1)
	erDistance := config.DefaultERDistance
	if c.ER.Distance != nil {
		erDistance = *c.ER.Distance
	}

	scaffolded := &ScaffoldConfig{
		Name:    c.Name,
		Desc:    c.Desc,
		Labels:  c.Labels,
		DSN:     c.DSN,
		DocPath: c.DocPath,
		Format: ScaffoldFormat{
			Adjust:                   c.Format.Adjust,
			Sort:                     c.Format.Sort,
			Number:                   c.Format.Number,
			ShowOnlyFirstParagraph:   c.Format.ShowOnlyFirstParagraph,
			HideColumnsWithoutValues: c.Format.HideColumnsWithoutValues,
		},
		ER: ScaffoldER{
			Skip:            c.ER.Skip,
			Format:          c.ER.Format,
			Comment:         c.ER.Comment,
			HideDef:         c.ER.HideDef,
			ShowColumnTypes: c.ER.ShowColumnTypes,
			Distance:        erDistance,
			Font:            c.ER.Font,
		},
		Include:     c.Include,
		Exclude:     c.Exclude,
		LintExclude: c.LintExclude,
		Lint: ScaffoldLint{
			RequireTableComment: ScaffoldRequireTableComment{
				Enabled:      c.Lint.RequireTableComment.Enabled,
				AllOrNothing: c.Lint.RequireTableComment.AllOrNothing,
				Exclude:      c.Lint.RequireTableComment.Exclude,
			},
			RequireColumnComment: ScaffoldRequireColumnComment{
				Enabled:       c.Lint.RequireColumnComment.Enabled,
				AllOrNothing:  c.Lint.RequireColumnComment.AllOrNothing,
				Exclude:       c.Lint.RequireColumnComment.Exclude,
				ExcludeTables: c.Lint.RequireColumnComment.ExcludeTables,
			},
			RequireIndexComment: ScaffoldRequireIndexComment{
				Enabled:       c.Lint.RequireIndexComment.Enabled,
				AllOrNothing:  c.Lint.RequireIndexComment.AllOrNothing,
				Exclude:       c.Lint.RequireIndexComment.Exclude,
				ExcludeTables: c.Lint.RequireIndexComment.ExcludeTables,
			},
			RequireConstraintComment: ScaffoldRequireConstraintComment{
				Enabled:       c.Lint.RequireConstraintComment.Enabled,
				AllOrNothing:  c.Lint.RequireConstraintComment.AllOrNothing,
				Exclude:       c.Lint.RequireConstraintComment.Exclude,
				ExcludeTables: c.Lint.RequireConstraintComment.ExcludeTables,
			},
			RequireTriggerComment: ScaffoldRequireTriggerComment{
				Enabled:       c.Lint.RequireTriggerComment.Enabled,
				AllOrNothing:  c.Lint.RequireTriggerComment.AllOrNothing,
				Exclude:       c.Lint.RequireTriggerComment.Exclude,
				ExcludeTables: c.Lint.RequireTriggerComment.ExcludeTables,
			},
			RequireTableLabels: ScaffoldRequireTableLabels{
				Enabled:      c.Lint.RequireTableLabels.Enabled,
				AllOrNothing: c.Lint.RequireTableLabels.AllOrNothing,
				Exclude:      c.Lint.RequireTableLabels.Exclude,
			},
			UnrelatedTable: ScaffoldUnrelatedTable{
				Enabled:      c.Lint.UnrelatedTable.Enabled,
				AllOrNothing: c.Lint.UnrelatedTable.AllOrNothing,
				Exclude:      c.Lint.UnrelatedTable.Exclude,
			},
			ColumnCount: ScaffoldColumnCount{
				Enabled: c.Lint.ColumnCount.Enabled,
				Max:     c.Lint.ColumnCount.Max,
				Exclude: c.Lint.ColumnCount.Exclude,
			},
			RequireColumns: ScaffoldRequireColumns{
				Enabled: c.Lint.RequireColumns.Enabled,
				Columns: c.Lint.RequireColumns.Columns,
			},
			DuplicateRelations: ScaffoldDuplicateRelations{
				Enabled: c.Lint.DuplicateRelations.Enabled,
			},
			RequireForeignKeyIndex: ScaffoldRequireForeignKeyIndex{
				Enabled: c.Lint.RequireForeignKeyIndex.Enabled,
				Exclude: c.Lint.RequireForeignKeyIndex.Exclude,
			},
			LabelStyleBigQuery: ScaffoldLabelStyleBigQuery{
				Enabled: c.Lint.LabelStyleBigQuery.Enabled,
				Exclude: c.Lint.LabelStyleBigQuery.Exclude,
			},
			RequireViewpoints: ScaffoldRequireViewpoints{
				Enabled: c.Lint.RequireViewpoints.Enabled,
				Exclude: c.Lint.RequireViewpoints.Exclude,
			},
		},
		Relations: relations,
		Comments:  comments,
		DetectVirtualRelations: ScaffoldDetectVirtualRelations{
			Enabled:  c.DetectVirtualRelations.Enabled,
			Strategy: c.DetectVirtualRelations.Strategy,
		},
		Stats: buildStatsConfig(c, s),
		BaseURL:             c.BaseURL,
		RequiredVersion:     c.RequiredVersion,
		DisableOutputSchema: c.DisableOutputSchema,
	}

	return scaffolded, nil
}

// buildCommentsFromSchema builds AdditionalComment list from schema.
// Uses database comments if available, empty string otherwise.
func buildCommentsFromSchema(s *schema.Schema, existingComments []config.AdditionalComment) []config.AdditionalComment {
	// Create a map of existing comments for quick lookup
	existingMap := make(map[string]*config.AdditionalComment)
	for i := range existingComments {
		existingMap[existingComments[i].Table] = &existingComments[i]
	}

	var comments []config.AdditionalComment

	for _, t := range s.Tables {
		ac := config.AdditionalComment{
			Table:              t.Name,
			TableComment:       t.Comment,
			ColumnComments:     make(map[string]string),
			IndexComments:      make(map[string]string),
			ConstraintComments: make(map[string]string),
			TriggerComments:    make(map[string]string),
		}

		// Check if there's an existing comment config for this table
		if existing, ok := existingMap[t.Name]; ok {
			// Preserve existing table comment if set
			if existing.TableComment != "" {
				ac.TableComment = existing.TableComment
			}
			// Preserve existing labels
			ac.Labels = existing.Labels
			ac.ColumnLabels = existing.ColumnLabels
		}

		// Columns
		for _, c := range t.Columns {
			comment := c.Comment
			// Check existing config
			if existing, ok := existingMap[t.Name]; ok {
				if existingComment, ok := existing.ColumnComments[c.Name]; ok && existingComment != "" {
					comment = existingComment
				}
			}
			ac.ColumnComments[c.Name] = comment
		}

		// Indexes
		for _, idx := range t.Indexes {
			comment := idx.Comment
			if existing, ok := existingMap[t.Name]; ok {
				if existingComment, ok := existing.IndexComments[idx.Name]; ok && existingComment != "" {
					comment = existingComment
				}
			}
			ac.IndexComments[idx.Name] = comment
		}

		// Constraints
		for _, cons := range t.Constraints {
			comment := cons.Comment
			if existing, ok := existingMap[t.Name]; ok {
				if existingComment, ok := existing.ConstraintComments[cons.Name]; ok && existingComment != "" {
					comment = existingComment
				}
			}
			ac.ConstraintComments[cons.Name] = comment
		}

		// Triggers
		for _, trig := range t.Triggers {
			comment := trig.Comment
			if existing, ok := existingMap[t.Name]; ok {
				if existingComment, ok := existing.TriggerComments[trig.Name]; ok && existingComment != "" {
					comment = existingComment
				}
			}
			ac.TriggerComments[trig.Name] = comment
		}

		comments = append(comments, ac)
	}

	return comments
}

// buildRelationsFromSchema builds AdditionalRelation list from schema.
// Includes existing relations from DB and infers new ones from naming patterns.
func buildRelationsFromSchema(s *schema.Schema, existingRelations []config.AdditionalRelation) []config.AdditionalRelation {
	// Create a set of existing relations for deduplication
	existingSet := make(map[string]bool)
	for _, r := range existingRelations {
		key := relationKey(r.Table, r.Columns, r.ParentTable, r.ParentColumns)
		existingSet[key] = true
	}

	// Also track relations already in the schema (from FK)
	schemaRelationSet := make(map[string]bool)
	for _, r := range s.Relations {
		columns := make([]string, len(r.Columns))
		for i, c := range r.Columns {
			columns[i] = c.Name
		}
		parentColumns := make([]string, len(r.ParentColumns))
		for i, c := range r.ParentColumns {
			parentColumns[i] = c.Name
		}
		key := relationKey(r.Table.Name, columns, r.ParentTable.Name, parentColumns)
		schemaRelationSet[key] = true
	}

	var relations []config.AdditionalRelation

	// Keep existing config relations
	relations = append(relations, existingRelations...)

	// Infer new relations from naming patterns
	strategy, _ := config.SelectNamingStrategy("default")

	for _, t := range s.Tables {
		for _, c := range t.Columns {
			// Try to infer parent table from column name
			parentTableName := strategy.ParentTableName(c.Name)
			if parentTableName == "" {
				continue
			}

			// Check if parent table exists
			parentTable, err := s.FindTableByName(parentTableName)
			if err != nil {
				continue
			}

			// Skip self-references
			if parentTable.Name == t.Name {
				continue
			}

			parentColumnName := strategy.ParentColumnName(c.Name)

			// Check if parent column exists
			_, err = parentTable.FindColumnByName(parentColumnName)
			if err != nil {
				continue
			}

			columns := []string{c.Name}
			parentColumns := []string{parentColumnName}
			key := relationKey(t.Name, columns, parentTable.Name, parentColumns)

			// Skip if already exists in config or schema
			if existingSet[key] || schemaRelationSet[key] {
				continue
			}

			// Add inferred relation
			relations = append(relations, config.AdditionalRelation{
				Table:         t.Name,
				Columns:       columns,
				ParentTable:   parentTable.Name,
				ParentColumns: parentColumns,
				Def:           "Inferred Relation",
			})

			existingSet[key] = true
		}
	}

	return relations
}

// relationKey creates a unique key for a relation for deduplication.
func relationKey(table string, columns []string, parentTable string, parentColumns []string) string {
	return fmt.Sprintf("%s.%v->%s.%v", table, columns, parentTable, parentColumns)
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

	// Add header comment and viewpoints example
	header := "# Generated by tbls scaffold\n# This config file contains all available parameters with their current/default values.\n# Modify as needed and remove this comment.\n\n"
	viewpointsExample := `
# Viewpoints allow you to organize tables into logical groups.
# Uncomment and modify the example below to define your viewpoints.
#
# viewpoints:
#   - name: example-viewpoint
#     desc: "Description of this viewpoint"
#     tables:
#       - table1
#       - table2
#     groups:
#       - name: group1
#         desc: "Description of this group"
#         tables:
#           - table1
`

	content := header + string(data) + viewpointsExample

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
	scaffoldCmd.Flags().BoolVarP(&force, "force", "f", false, "force overwrite without prompt")
}
