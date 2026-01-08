package drivers

import (
	"context"

	"github.com/k1LoW/tbls/schema"
)

// Driver is the common interface for database drivers.
type Driver interface {
	Analyze(ctx context.Context, s *schema.Schema) error
	Info() (*schema.Driver, error)
}

// StatsCollector is an optional interface for drivers that support statistics collection
type StatsCollector interface {
	CollectStats(s *schema.Schema, cfg StatsConfig) error
}

// DateColumnDetector is an optional interface for drivers that can detect date columns
// for partition-based filtering during stats collection
type DateColumnDetector interface {
	// DetectDateColumns returns a map of table name to detected date column name
	// The detected date column is used for partition filtering during stats collection
	DetectDateColumns(s *schema.Schema) (map[string]string, error)
}

// ProgressReporter is an interface for reporting stats collection progress
type ProgressReporter interface {
	// ReportColumn reports progress at column level
	ReportColumn(tableName, columnName string, completedColumns, totalColumns int)
	// IsCancelled returns true if the operation should be cancelled
	IsCancelled() bool
}

// CheckpointUpdater is an interface for updating checkpoints during stats collection
type CheckpointUpdater interface {
	// UpdateColumn is called when a column's stats have been collected
	UpdateColumn(tableName, columnName string, stats *schema.ColumnStats)
	// MarkTableCompleted is called when all columns of a table have been processed
	MarkTableCompleted(tableName string)
	// IsColumnCompleted returns true if the column was already processed
	IsColumnCompleted(tableName, columnName string) bool
	// IsTableCompleted returns true if the table was already fully processed
	IsTableCompleted(tableName string) bool
	// Save persists the current checkpoint state
	Save() error
	// MarkCompleted marks the entire stats collection as completed (for cache reuse)
	MarkCompleted()
}

// StatsConfig is passed to StatsCollector
type StatsConfig struct {
	Include             []string
	Exclude             []string
	TopN                int
	SampleSize          int
	LargeTableThreshold int64
	RecentDays          int

	// Progress and checkpoint support (optional)
	Progress   ProgressReporter
	Checkpoint CheckpointUpdater

	// Per-table sampling configuration
	Tables []TableStatsConfig

	// Context for cancellation support
	Ctx context.Context
}

// SamplingMode defines how a table should be sampled for stats collection
type SamplingMode string

const (
	// SamplingModeDateFilter samples data within a date range (uses dateColumn + recentDays)
	SamplingModeDateFilter SamplingMode = "date_filter"
	// SamplingModeRowLimit samples first N rows (uses sampleSize, -1 means no limit)
	SamplingModeRowLimit SamplingMode = "row_limit"
)

// TableStatsConfig holds per-table stats configuration with explicit sampling mode.
// Mode determines how the table is sampled:
//   - date_filter: query recent data within RecentDays using DateColumn
//   - row_limit: query first SampleSize rows (-1 means no limit)
type TableStatsConfig struct {
	Name       string
	Mode       SamplingMode
	DateColumn string
	SampleSize int
}

// GetTableConfig returns the config for a specific table, or nil if not found
func (c *StatsConfig) GetTableConfig(tableName string) *TableStatsConfig {
	for i := range c.Tables {
		if c.Tables[i].Name == tableName {
			return &c.Tables[i]
		}
	}
	return nil
}

// Option is the type for change Config.
type Option func(Driver) error
