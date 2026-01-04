package stats

import (
	"github.com/k1LoW/tbls/drivers"
	"github.com/k1LoW/tbls/schema"
)

// ProgressAdapter adapts ProgressReporter to drivers.ProgressReporter
type ProgressAdapter struct {
	reporter ProgressReporter
}

// NewProgressAdapter creates a new ProgressAdapter
func NewProgressAdapter(reporter ProgressReporter) *ProgressAdapter {
	return &ProgressAdapter{reporter: reporter}
}

// ReportColumn implements drivers.ProgressReporter
func (a *ProgressAdapter) ReportColumn(tableName, columnName string, completedColumns, totalColumns int) {
	a.reporter.Report(Progress{
		Stage:            StageCollectingStats,
		CurrentTable:     tableName,
		CurrentColumn:    columnName,
		CompletedColumns: completedColumns,
		TotalColumns:     totalColumns,
	})
}

// IsCancelled implements drivers.ProgressReporter
func (a *ProgressAdapter) IsCancelled() bool {
	return a.reporter.IsCancelled()
}

// CheckpointAdapter adapts CheckpointManager to drivers.CheckpointUpdater
type CheckpointAdapter struct {
	manager *CheckpointManager
	cp      *Checkpoint
}

// NewCheckpointAdapter creates a new CheckpointAdapter
func NewCheckpointAdapter(manager *CheckpointManager, cp *Checkpoint) *CheckpointAdapter {
	return &CheckpointAdapter{
		manager: manager,
		cp:      cp,
	}
}

// UpdateColumn implements drivers.CheckpointUpdater
func (a *CheckpointAdapter) UpdateColumn(tableName, columnName string, stats *schema.ColumnStats) {
	UpdateCheckpointWithColumnStats(a.cp, tableName, columnName, stats)
}

// MarkTableCompleted implements drivers.CheckpointUpdater
func (a *CheckpointAdapter) MarkTableCompleted(tableName string) {
	MarkTableCompleted(a.cp, tableName)
}

// IsColumnCompleted implements drivers.CheckpointUpdater
func (a *CheckpointAdapter) IsColumnCompleted(tableName, columnName string) bool {
	return IsColumnCompleted(a.cp, tableName, columnName)
}

// IsTableCompleted implements drivers.CheckpointUpdater
func (a *CheckpointAdapter) IsTableCompleted(tableName string) bool {
	return IsTableCompleted(a.cp, tableName)
}

// Save implements drivers.CheckpointUpdater
func (a *CheckpointAdapter) Save() error {
	return a.manager.Save(a.cp)
}

// Checkpoint returns the underlying checkpoint
func (a *CheckpointAdapter) Checkpoint() *Checkpoint {
	return a.cp
}

// Verify interface compliance
var _ drivers.ProgressReporter = (*ProgressAdapter)(nil)
var _ drivers.CheckpointUpdater = (*CheckpointAdapter)(nil)
