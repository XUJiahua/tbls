package drivers

import (
	"github.com/k1LoW/tbls/schema"
)

// Driver is the common interface for database drivers.
type Driver interface {
	Analyze(*schema.Schema) error
	Info() (*schema.Driver, error)
}

// StatsCollector is an optional interface for drivers that support statistics collection
type StatsCollector interface {
	CollectStats(s *schema.Schema, cfg StatsConfig) error
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

	// Date column configuration for partition filtering
	DateColumn string
	Tables     map[string]TableStatsConfig
}

// TableStatsConfig holds per-table stats configuration
type TableStatsConfig struct {
	DateColumn string
	Skip       bool
}

// Option is the type for change Config.
type Option func(Driver) error
