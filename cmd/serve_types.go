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
	// Relations defines additional relations to add
	Relations []RelationConfig `json:"relations,omitempty"`
	// Comments defines additional comments to add
	Comments []CommentConfig `json:"comments,omitempty"`
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

// RelationConfig defines a table relation
// @Description Table relation configuration
type RelationConfig struct {
	// Table is the child table name
	Table string `json:"table" example:"posts"`
	// Columns are the child table columns
	Columns []string `json:"columns" example:"author_id"`
	// ParentTable is the parent table name
	ParentTable string `json:"parentTable" example:"users"`
	// ParentColumns are the parent table columns
	ParentColumns []string `json:"parentColumns" example:"id"`
	// Def is the relation definition/description
	Def string `json:"def,omitempty"`
	// Cardinality describes the relationship cardinality
	Cardinality string `json:"cardinality,omitempty" enums:"zero or one,exactly one,zero or more,one or more"`
}

// CommentConfig defines table/column comments
// @Description Table and column comments configuration
type CommentConfig struct {
	// Table is the table name
	Table string `json:"table" example:"users"`
	// TableComment is the comment for the table
	TableComment string `json:"tableComment,omitempty" example:"User accounts table"`
	// ColumnComments maps column names to their comments
	ColumnComments map[string]string `json:"columnComments,omitempty"`
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

	for _, rel := range r.Relations {
		cfg.Relations = append(cfg.Relations, config.AdditionalRelation{
			Table:         rel.Table,
			Columns:       rel.Columns,
			ParentTable:   rel.ParentTable,
			ParentColumns: rel.ParentColumns,
			Def:           rel.Def,
		})
	}

	for _, c := range r.Comments {
		cfg.Comments = append(cfg.Comments, config.AdditionalComment{
			Table:          c.Table,
			TableComment:   c.TableComment,
			ColumnComments: c.ColumnComments,
		})
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
