package stats

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/k1LoW/tbls/checkpoint"
	"github.com/k1LoW/tbls/schema"
)

const (
	CheckpointVersion = 1
)

// ColumnStatsCheckpoint stores partial column stats
type ColumnStatsCheckpoint struct {
	Stats *schema.ColumnStats `json:"stats,omitempty"`
}

// TableStatsCheckpoint stores partial table stats with column-level progress
type TableStatsCheckpoint struct {
	Stats   *schema.TableStats                `json:"stats,omitempty"`
	Columns map[string]*ColumnStatsCheckpoint `json:"columns,omitempty"`
}

// CheckpointProgress tracks the progress of stats collection
type CheckpointProgress struct {
	CompletedTables  []string `json:"completed_tables"`
	CurrentTable     string   `json:"current_table,omitempty"`
	CompletedColumns []string `json:"completed_columns,omitempty"`
	TotalColumns     int      `json:"total_columns,omitempty"`
}

// Checkpoint represents a saved checkpoint for resumable stats collection
type Checkpoint struct {
	Version       int                              `json:"version"`
	TaskID        string                           `json:"task_id,omitempty"`
	DSNHash       string                           `json:"dsn_hash"`
	SchemaHash    string                           `json:"schema_hash"`
	UpdatedAt     time.Time                        `json:"updated_at"`
	Stage         Stage                            `json:"stage"`
	Progress      CheckpointProgress               `json:"progress"`
	PartialResult map[string]*TableStatsCheckpoint `json:"partial_result,omitempty"`
}

// CheckpointManager manages checkpoint files
type CheckpointManager struct {
	dsnURL string
	ttl    time.Duration
	force  bool
}

// NewCheckpointManager creates a new checkpoint manager.
// dsnURL is used to determine the checkpoint file path via hash.
func NewCheckpointManager(dsnURL string, ttl time.Duration, force bool) *CheckpointManager {
	return &CheckpointManager{
		dsnURL: dsnURL,
		ttl:    ttl,
		force:  force,
	}
}

// checkpointPath returns the full path to the checkpoint file
func (m *CheckpointManager) checkpointPath() string {
	return checkpoint.GetCheckpointPath(m.dsnURL)
}

// DSNHash returns the hash of the DSN URL used for this checkpoint
func (m *CheckpointManager) DSNHash() string {
	return checkpoint.HashDSNURL(m.dsnURL)
}

// Load loads a checkpoint if it exists and is valid
// Returns nil if no valid checkpoint exists
func (m *CheckpointManager) Load(dsnHash, schemaHash string) (*Checkpoint, error) {
	if m.force {
		return nil, nil
	}

	path := m.checkpointPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read checkpoint: %w", err)
	}

	var cp Checkpoint
	if err := json.Unmarshal(data, &cp); err != nil {
		// Checkpoint is corrupted, ignore it
		return nil, nil
	}

	// Validate version
	if cp.Version != CheckpointVersion {
		return nil, nil
	}

	// Validate DSN
	if cp.DSNHash != dsnHash {
		return nil, nil
	}

	// Validate schema
	if cp.SchemaHash != schemaHash {
		return nil, nil
	}

	// Validate TTL
	if time.Since(cp.UpdatedAt) > m.ttl {
		return nil, nil
	}

	return &cp, nil
}

// Save saves a checkpoint to disk
func (m *CheckpointManager) Save(cp *Checkpoint) error {
	// Ensure checkpoint directory exists
	if err := checkpoint.EnsureCheckpointDir(); err != nil {
		return fmt.Errorf("failed to create checkpoint directory: %w", err)
	}

	cp.UpdatedAt = time.Now()
	cp.Version = CheckpointVersion

	data, err := json.MarshalIndent(cp, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal checkpoint: %w", err)
	}

	path := m.checkpointPath()
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write checkpoint: %w", err)
	}

	return nil
}

// Delete removes the checkpoint file
func (m *CheckpointManager) Delete() error {
	path := m.checkpointPath()
	err := os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete checkpoint: %w", err)
	}
	return nil
}

// HashDSN creates a hash of the DSN for checkpoint validation
func HashDSN(dsn string) string {
	h := sha256.Sum256([]byte(dsn))
	return hex.EncodeToString(h[:])
}

// HashSchema creates a hash of the schema structure (table names and column names)
func HashSchema(s *schema.Schema) string {
	var parts []string

	// Sort tables by name for consistent hashing
	tables := make([]*schema.Table, len(s.Tables))
	copy(tables, s.Tables)
	sort.Slice(tables, func(i, j int) bool {
		return tables[i].Name < tables[j].Name
	})

	for _, t := range tables {
		parts = append(parts, t.Name)
		// Sort columns by name
		columns := make([]*schema.Column, len(t.Columns))
		copy(columns, t.Columns)
		sort.Slice(columns, func(i, j int) bool {
			return columns[i].Name < columns[j].Name
		})
		for _, c := range columns {
			parts = append(parts, fmt.Sprintf("%s.%s", t.Name, c.Name))
		}
	}

	h := sha256.Sum256([]byte(fmt.Sprintf("%v", parts)))
	return hex.EncodeToString(h[:])
}

// ApplyCheckpoint applies partial results from a checkpoint to the schema
func ApplyCheckpoint(s *schema.Schema, cp *Checkpoint) {
	if cp == nil || cp.PartialResult == nil {
		return
	}

	for _, t := range s.Tables {
		tableCP, ok := cp.PartialResult[t.Name]
		if !ok {
			continue
		}

		// Apply table stats
		if tableCP.Stats != nil {
			t.Stats = tableCP.Stats
		}

		// Apply column stats
		if tableCP.Columns != nil {
			for _, c := range t.Columns {
				colCP, ok := tableCP.Columns[c.Name]
				if ok && colCP.Stats != nil {
					c.Stats = colCP.Stats
				}
			}
		}
	}
}

// UpdateCheckpointWithColumnStats updates the checkpoint with new column stats
func UpdateCheckpointWithColumnStats(cp *Checkpoint, tableName, columnName string, stats *schema.ColumnStats) {
	if cp.PartialResult == nil {
		cp.PartialResult = make(map[string]*TableStatsCheckpoint)
	}

	if cp.PartialResult[tableName] == nil {
		cp.PartialResult[tableName] = &TableStatsCheckpoint{
			Columns: make(map[string]*ColumnStatsCheckpoint),
		}
	}

	if cp.PartialResult[tableName].Columns == nil {
		cp.PartialResult[tableName].Columns = make(map[string]*ColumnStatsCheckpoint)
	}

	cp.PartialResult[tableName].Columns[columnName] = &ColumnStatsCheckpoint{
		Stats: stats,
	}

	// Update progress
	cp.Progress.CurrentTable = tableName
	if !contains(cp.Progress.CompletedColumns, columnName) {
		cp.Progress.CompletedColumns = append(cp.Progress.CompletedColumns, columnName)
	}
}

// UpdateCheckpointWithTableStats updates the checkpoint with table stats
func UpdateCheckpointWithTableStats(cp *Checkpoint, tableName string, stats *schema.TableStats) {
	if cp.PartialResult == nil {
		cp.PartialResult = make(map[string]*TableStatsCheckpoint)
	}

	if cp.PartialResult[tableName] == nil {
		cp.PartialResult[tableName] = &TableStatsCheckpoint{
			Columns: make(map[string]*ColumnStatsCheckpoint),
		}
	}

	cp.PartialResult[tableName].Stats = stats
}

// MarkTableCompleted marks a table as completed in the checkpoint
func MarkTableCompleted(cp *Checkpoint, tableName string) {
	if !contains(cp.Progress.CompletedTables, tableName) {
		cp.Progress.CompletedTables = append(cp.Progress.CompletedTables, tableName)
	}
	cp.Progress.CurrentTable = ""
	cp.Progress.CompletedColumns = nil
}

// IsTableCompleted returns true if the table has been fully processed
func IsTableCompleted(cp *Checkpoint, tableName string) bool {
	return contains(cp.Progress.CompletedTables, tableName)
}

// IsColumnCompleted returns true if the column has been processed
func IsColumnCompleted(cp *Checkpoint, tableName, columnName string) bool {
	if cp.Progress.CurrentTable != tableName {
		return false
	}
	return contains(cp.Progress.CompletedColumns, columnName)
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
