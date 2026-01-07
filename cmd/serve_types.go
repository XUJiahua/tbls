package cmd

import (
	"time"

	"github.com/k1LoW/tbls/config"
	"github.com/k1LoW/tbls/schema"
	"github.com/k1LoW/tbls/stats"
)

// SchemaRequest is the request body for POST /schema endpoint
// @Description Request body for submitting a schema analysis task
type SchemaRequest struct {
	// DSN contains the database connection configuration
	DSN DSNConfig `json:"dsn" binding:"required"`
	// Name overrides the database name
	Name string `json:"name,omitempty"`
	// Desc is the database description
	Desc string `json:"desc,omitempty"`
	// Include specifies tables to include (supports wildcards)
	Include []string `json:"include,omitempty" example:"users,posts,comments_*"`
	// Exclude specifies tables to exclude (supports wildcards)
	Exclude []string `json:"exclude,omitempty" example:"*_backup,*_temp,*_log"`
	// Distance is the relation distance for filtering
	Distance int `json:"distance,omitempty"`
	// Format contains output format settings
	Format *FormatConfig `json:"format,omitempty"`
	// DetectVirtualRelations configures virtual relation detection
	DetectVirtualRelations *DetectVirtualRelationsConfig `json:"detectVirtualRelations,omitempty"`
	// Stats configures statistics collection
	Stats *StatsConfig `json:"stats,omitempty"`
	// Force ignores checkpoint and forces stats collection
	Force bool `json:"force,omitempty"`
}

// DSNConfig contains database connection configuration
// @Description Database connection configuration
type DSNConfig struct {
	// URL is the database connection string (required)
	URL string `json:"url" binding:"required" example:"postgres://user:pass@localhost:5432/dbname"`
	// Headers are custom headers for HTTP-based connections
	Headers map[string]string `json:"headers,omitempty"`
}

// FormatConfig contains output format settings
// @Description Output format settings
type FormatConfig struct {
	// Sort tables and columns alphabetically
	Sort bool `json:"sort,omitempty"`
	// Adjust column widths
	Adjust bool `json:"adjust,omitempty"`
}

// DetectVirtualRelationsConfig configures virtual relation detection
// @Description Virtual relation detection configuration
type DetectVirtualRelationsConfig struct {
	// Enabled enables virtual relation detection
	Enabled bool `json:"enabled,omitempty"`
	// Strategy is the naming strategy (default, rails, laravel)
	Strategy string `json:"strategy,omitempty" enums:"default,rails,laravel"`
}

// StatsConfig configures statistics collection
// @Description Statistics collection configuration
type StatsConfig struct {
	// Enabled enables statistics collection
	Enabled bool `json:"enabled,omitempty"`
	// Include specifies tables to collect stats for (supports wildcards)
	Include []string `json:"include,omitempty"`
	// Exclude specifies tables to exclude from stats collection
	Exclude []string `json:"exclude,omitempty"`
	// TopN is the number of top values to collect per column
	TopN int `json:"topN,omitempty" example:"10"`
	// SampleSize is the maximum rows to sample
	SampleSize int `json:"sampleSize,omitempty" example:"10000"`
	// LargeTableThreshold is the row count threshold for large table sampling
	LargeTableThreshold int64 `json:"largeTableThreshold,omitempty" example:"1000000"`
	// RecentDays is days to look back for large table sampling
	RecentDays int `json:"recentDays,omitempty" example:"30"`
	// Checkpoint configures checkpoint/resume functionality
	Checkpoint *CheckpointConfig `json:"checkpoint,omitempty"`
}

// CheckpointConfig configures checkpoint/resume functionality
// @Description Checkpoint/resume configuration
type CheckpointConfig struct {
	// Enabled enables checkpoint/resume functionality
	Enabled bool `json:"enabled,omitempty"`
	// TTL is the checkpoint validity duration (Go duration format)
	TTL string `json:"ttl,omitempty" example:"24h"`
	// Force ignores existing checkpoint and starts fresh
	Force bool `json:"force,omitempty"`
}

// TaskAcceptedResponse is returned when a task is accepted
// @Description Response when a schema analysis task is accepted
type TaskAcceptedResponse struct {
	// TaskID is the unique task identifier for polling
	TaskID string `json:"task_id" example:"550e8400-e29b-41d4-a716-446655440000"`
	// Status is the initial task status
	Status string `json:"status" example:"pending"`
}

// TaskExistsResponse is returned when a task already exists for the DSN
// @Description Response when a task is already running for the same DSN
type TaskExistsResponse struct {
	// TaskID is the existing task identifier
	TaskID string `json:"task_id" example:"550e8400-e29b-41d4-a716-446655440000"`
	// Status is the existing task status
	Status string `json:"status" example:"running"`
	// Message explains why a new task was not created
	Message string `json:"message" example:"task already running for this DSN"`
}

// TaskStatusResponse contains task status and progress information
// @Description Task status and progress information
type TaskStatusResponse struct {
	// TaskID is the task identifier
	TaskID string `json:"task_id" example:"550e8400-e29b-41d4-a716-446655440000"`
	// Status is the current task status (pending, running, completed, failed, cancelled)
	Status string `json:"status" example:"running" enums:"pending,running,completed,failed,cancelled"`
	// Stage is the current processing stage
	Stage stats.Stage `json:"stage,omitempty" example:"collecting_stats" enums:"analyzing,collecting_stats,inferring,completed,failed,cancelled"`
	// Progress contains detailed progress information
	Progress *ProgressInfo `json:"progress,omitempty"`
	// ResumedFromCheckpoint indicates if task was resumed from checkpoint
	ResumedFromCheckpoint bool `json:"resumed_from_checkpoint,omitempty"`
	// StartedAt is the task start time
	StartedAt time.Time `json:"started_at,omitempty" example:"2025-01-04T10:30:00Z"`
	// CompletedAt is the task completion time
	CompletedAt time.Time `json:"completed_at,omitempty" example:"2025-01-04T10:35:00Z"`
	// Error contains the error message if failed
	Error string `json:"error,omitempty" example:"connection refused"`
	// Result contains the schema analysis result
	Result *schema.Schema `json:"result,omitempty"`
}

// ProgressInfo contains detailed progress information
// @Description Detailed progress information during stats collection
type ProgressInfo struct {
	// CurrentTable is the currently processing table
	CurrentTable string `json:"current_table,omitempty" example:"orders"`
	// CurrentColumn is the currently processing column
	CurrentColumn string `json:"current_column,omitempty" example:"customer_id"`
	// CompletedColumns is the number of columns completed
	CompletedColumns int `json:"completed_columns,omitempty" example:"45"`
	// TotalColumns is the total number of columns to process
	TotalColumns int `json:"total_columns,omitempty" example:"200"`
}

// TaskCancelledResponse is returned when a task is cancelled
// @Description Response when a task is cancelled
type TaskCancelledResponse struct {
	// TaskID is the task identifier
	TaskID string `json:"task_id" example:"550e8400-e29b-41d4-a716-446655440000"`
	// Status is the task status after cancellation
	Status string `json:"status" example:"cancelled"`
	// CheckpointSaved indicates if progress was saved to checkpoint
	CheckpointSaved bool `json:"checkpoint_saved" example:"true"`
}

// TaskNotRunningError is returned when trying to cancel a non-running task
// @Description Error response when task is not running
type TaskNotRunningError struct {
	// Error is the error message
	Error string `json:"error" example:"task is not running"`
	// Status is the current task status
	Status string `json:"status" example:"completed"`
	// TaskID is the task identifier
	TaskID string `json:"task_id" example:"550e8400-e29b-41d4-a716-446655440000"`
}

// ErrorResponse is a generic error response
// @Description Generic error response
type ErrorResponse struct {
	// Error is the error message
	Error string `json:"error" example:"dsn.url is required"`
}

// ScaffoldRequest is the request body for POST /scaffold endpoint
// @Description Request body for generating a scaffolded config
type ScaffoldRequest struct {
	// DSN contains the database connection configuration (required)
	DSN DSNConfig `json:"dsn" binding:"required"`
}

// ScaffoldResponse is the response for scaffold endpoint
// @Description Response containing the scaffolded configuration
type ScaffoldResponse struct {
	// Config is the generated configuration with all parameters filled in
	Config *APIScaffoldConfig `json:"config"`
}

// APIScaffoldConfig is the scaffolded config structure for API response
// @Description Complete configuration with all parameters and defaults
type APIScaffoldConfig struct {
	Name                   string                            `json:"name,omitempty"`
	Desc                   string                            `json:"desc,omitempty"`
	Labels                 []string                          `json:"labels,omitempty"`
	DSN                    config.DSN                        `json:"dsn"`
	DocPath                string                            `json:"docPath"`
	Format                 APIScaffoldFormatConfig           `json:"format"`
	ER                     APIScaffoldERConfig               `json:"er"`
	Include                []string                          `json:"include,omitempty"`
	Exclude                []string                          `json:"exclude,omitempty"`
	DetectVirtualRelations APIScaffoldDetectVirtualRelConfig `json:"detectVirtualRelations"`
	Stats                  APIScaffoldStatsConfig            `json:"stats"`
	BaseURL                string                            `json:"baseUrl,omitempty"`
	RequiredVersion        string                            `json:"requiredVersion,omitempty"`
}

// APIScaffoldFormatConfig represents format settings
// @Description Document format settings
type APIScaffoldFormatConfig struct {
	Adjust                   bool     `json:"adjust"`
	Sort                     bool     `json:"sort"`
	Number                   bool     `json:"number"`
	ShowOnlyFirstParagraph   bool     `json:"showOnlyFirstParagraph"`
	HideColumnsWithoutValues []string `json:"hideColumnsWithoutValues,omitempty"`
}

// APIScaffoldERConfig represents ER diagram settings
// @Description ER diagram generation settings
type APIScaffoldERConfig struct {
	Skip            bool                    `json:"skip"`
	Format          string                  `json:"format" example:"svg"`
	Comment         bool                    `json:"comment"`
	HideDef         bool                    `json:"hideDef"`
	ShowColumnTypes *config.ShowColumnTypes `json:"showColumnTypes,omitempty"`
	Distance        int                     `json:"distance" example:"1"`
	Font            string                  `json:"font,omitempty"`
}

// APIScaffoldDetectVirtualRelConfig represents virtual relation detection settings
// @Description Virtual relation detection settings
type APIScaffoldDetectVirtualRelConfig struct {
	Enabled  bool   `json:"enabled"`
	Strategy string `json:"strategy,omitempty"`
}

// APIScaffoldStatsConfig represents statistics collection settings
// @Description Statistics collection configuration
type APIScaffoldStatsConfig struct {
	Enabled             bool                                  `json:"enabled"`
	Include             []string                              `json:"include,omitempty"`
	Exclude             []string                              `json:"exclude,omitempty"`
	TopN                int                                   `json:"topN" example:"10"`
	SampleSize          int                                   `json:"sampleSize" example:"10000"`
	LargeTableThreshold int64                                 `json:"largeTableThreshold" example:"1000000"`
	RecentDays          int                                   `json:"recentDays" example:"30"`
	DateColumn          string                                `json:"dateColumn,omitempty"`
	Inference           APIScaffoldInferenceConfig            `json:"inference"`
	Checkpoint          APIScaffoldCheckpointConfig           `json:"checkpoint"`
	Tables              map[string]APIScaffoldTableStatConfig `json:"tables,omitempty"`
}

// APIScaffoldInferenceConfig represents inference settings
// @Description Stats-based inference configuration
type APIScaffoldInferenceConfig struct {
	Enabled                 bool    `json:"enabled"`
	EnumMaxCardinality      float64 `json:"enumMaxCardinality" example:"0.01"`
	EnumMaxDistinct         int     `json:"enumMaxDistinct" example:"20"`
	DictMaxCardinality      float64 `json:"dictMaxCardinality" example:"0.05"`
	DictMaxDistinct         int     `json:"dictMaxDistinct" example:"100"`
	ForeignKeyMinConfidence float64 `json:"foreignKeyMinConfidence" example:"0.7"`
}

// APIScaffoldCheckpointConfig represents checkpoint settings
// @Description Checkpoint/resume configuration
type APIScaffoldCheckpointConfig struct {
	Enabled bool   `json:"enabled"`
	TTL     string `json:"ttl" example:"24h"`
	Force   bool   `json:"force"`
}

// APIScaffoldTableStatConfig represents per-table stats settings
// @Description Per-table statistics configuration
type APIScaffoldTableStatConfig struct {
	DateColumn string `json:"dateColumn,omitempty"`
	Skip       bool   `json:"skip"`
}

// toConfig converts SchemaRequest to config.Config
func (r *SchemaRequest) toConfig() config.Config {
	cfg := config.Config{
		DSN: config.DSN{
			URL:     r.DSN.URL,
			Headers: r.DSN.Headers,
		},
		Name:     r.Name,
		Desc:     r.Desc,
		Include:  r.Include,
		Exclude:  r.Exclude,
		Distance: r.Distance,
	}

	if r.Format != nil {
		cfg.Format.Sort = r.Format.Sort
		cfg.Format.Adjust = r.Format.Adjust
	}

	if r.DetectVirtualRelations != nil {
		cfg.DetectVirtualRelations.Enabled = r.DetectVirtualRelations.Enabled
		cfg.DetectVirtualRelations.Strategy = r.DetectVirtualRelations.Strategy
	}

	if r.Stats != nil {
		cfg.Stats.Enabled = r.Stats.Enabled
		cfg.Stats.Include = r.Stats.Include
		cfg.Stats.Exclude = r.Stats.Exclude
		cfg.Stats.TopN = r.Stats.TopN
		cfg.Stats.SampleSize = r.Stats.SampleSize
		cfg.Stats.LargeTableThreshold = r.Stats.LargeTableThreshold
		cfg.Stats.RecentDays = r.Stats.RecentDays

		if r.Stats.Checkpoint != nil {
			cfg.Stats.Checkpoint.Enabled = r.Stats.Checkpoint.Enabled
			cfg.Stats.Checkpoint.TTL = r.Stats.Checkpoint.TTL
			cfg.Stats.Checkpoint.Force = r.Stats.Checkpoint.Force
		}
	}

	if r.Force {
		cfg.Stats.Checkpoint.Force = true
	}

	return cfg
}
