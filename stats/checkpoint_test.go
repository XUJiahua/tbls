package stats

import (
	"os"
	"testing"
	"time"

	"github.com/k1LoW/tbls/checkpoint"
	"github.com/k1LoW/tbls/schema"
)

const testDSNURL = "postgres://test:test@localhost/testdb"

func TestCheckpointManager(t *testing.T) {
	// Clean up any existing checkpoint before and after tests
	cleanup := func() {
		checkpoint.DeleteCheckpoint(testDSNURL)
	}
	cleanup()
	defer cleanup()

	t.Run("Save and Load", func(t *testing.T) {
		manager := NewCheckpointManager(testDSNURL, 24*time.Hour, false)

		cp := &Checkpoint{
			DSNHash:    manager.DSNHash(),
			SchemaHash: "test-schema-hash",
			Stage:      StageCollectingStats,
			Progress: CheckpointProgress{
				CompletedTables:  []string{"users"},
				CurrentTable:     "orders",
				CompletedColumns: []string{"id", "user_id"},
				TotalColumns:     10,
			},
		}

		// Save checkpoint
		if err := manager.Save(cp); err != nil {
			t.Fatalf("Failed to save checkpoint: %v", err)
		}

		// Verify file exists
		path := checkpoint.GetCheckpointPath(testDSNURL)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Fatal("Checkpoint file not created")
		}

		// Load checkpoint
		loaded, err := manager.Load(manager.DSNHash(), "test-schema-hash")
		if err != nil {
			t.Fatalf("Failed to load checkpoint: %v", err)
		}

		if loaded == nil {
			t.Fatal("Expected checkpoint to be loaded")
		}

		if loaded.Stage != StageCollectingStats {
			t.Errorf("Expected stage %s, got %s", StageCollectingStats, loaded.Stage)
		}

		if len(loaded.Progress.CompletedTables) != 1 {
			t.Errorf("Expected 1 completed table, got %d", len(loaded.Progress.CompletedTables))
		}
	})

	t.Run("Load with wrong DSN hash", func(t *testing.T) {
		manager := NewCheckpointManager(testDSNURL, 24*time.Hour, false)

		loaded, err := manager.Load("wrong-dsn-hash", "test-schema-hash")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if loaded != nil {
			t.Error("Expected nil checkpoint for wrong DSN hash")
		}
	})

	t.Run("Load with wrong schema hash", func(t *testing.T) {
		manager := NewCheckpointManager(testDSNURL, 24*time.Hour, false)

		loaded, err := manager.Load(manager.DSNHash(), "wrong-schema-hash")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if loaded != nil {
			t.Error("Expected nil checkpoint for wrong schema hash")
		}
	})

	t.Run("Load with force flag", func(t *testing.T) {
		manager := NewCheckpointManager(testDSNURL, 24*time.Hour, true) // force = true

		loaded, err := manager.Load(manager.DSNHash(), "test-schema-hash")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if loaded != nil {
			t.Error("Expected nil checkpoint when force is true")
		}
	})

	t.Run("Delete checkpoint", func(t *testing.T) {
		manager := NewCheckpointManager(testDSNURL, 24*time.Hour, false)

		// Delete checkpoint
		if err := manager.Delete(); err != nil {
			t.Fatalf("Failed to delete checkpoint: %v", err)
		}

		// Verify file is deleted
		path := checkpoint.GetCheckpointPath(testDSNURL)
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Error("Expected checkpoint file to be deleted")
		}
	})

	t.Run("Load expired checkpoint", func(t *testing.T) {
		manager := NewCheckpointManager(testDSNURL, 1*time.Millisecond, false)

		cp := &Checkpoint{
			DSNHash:    manager.DSNHash(),
			SchemaHash: "test-schema-hash",
			Stage:      StageCollectingStats,
		}

		if err := manager.Save(cp); err != nil {
			t.Fatalf("Failed to save checkpoint: %v", err)
		}

		// Wait for checkpoint to expire
		time.Sleep(10 * time.Millisecond)

		loaded, err := manager.Load(manager.DSNHash(), "test-schema-hash")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if loaded != nil {
			t.Error("Expected nil checkpoint for expired TTL")
		}
	})
}

func TestHashDSN(t *testing.T) {
	hash1 := HashDSN("postgres://user:pass@localhost/db")
	hash2 := HashDSN("postgres://user:pass@localhost/db")
	hash3 := HashDSN("postgres://user:pass@localhost/other")

	if hash1 != hash2 {
		t.Error("Same DSN should produce same hash")
	}

	if hash1 == hash3 {
		t.Error("Different DSN should produce different hash")
	}
}

func TestHashSchema(t *testing.T) {
	s1 := &schema.Schema{
		Tables: []*schema.Table{
			{Name: "users", Columns: []*schema.Column{{Name: "id"}, {Name: "name"}}},
		},
	}

	s2 := &schema.Schema{
		Tables: []*schema.Table{
			{Name: "users", Columns: []*schema.Column{{Name: "id"}, {Name: "name"}}},
		},
	}

	s3 := &schema.Schema{
		Tables: []*schema.Table{
			{Name: "users", Columns: []*schema.Column{{Name: "id"}, {Name: "email"}}},
		},
	}

	hash1 := HashSchema(s1)
	hash2 := HashSchema(s2)
	hash3 := HashSchema(s3)

	if hash1 != hash2 {
		t.Error("Same schema should produce same hash")
	}

	if hash1 == hash3 {
		t.Error("Different schema should produce different hash")
	}
}

func TestApplyCheckpoint(t *testing.T) {
	s := &schema.Schema{
		Tables: []*schema.Table{
			{
				Name: "users",
				Columns: []*schema.Column{
					{Name: "id"},
					{Name: "name"},
				},
			},
		},
	}

	cp := &Checkpoint{
		PartialResult: map[string]*TableStatsCheckpoint{
			"users": {
				Stats: &schema.TableStats{RowCount: 100},
				Columns: map[string]*ColumnStatsCheckpoint{
					"id": {Stats: &schema.ColumnStats{RowCount: 100, DistinctCount: 100}},
				},
			},
		},
	}

	ApplyCheckpoint(s, cp)

	if s.Tables[0].Stats == nil {
		t.Error("Table stats should be applied")
	}

	if s.Tables[0].Stats.RowCount != 100 {
		t.Errorf("Expected row count 100, got %d", s.Tables[0].Stats.RowCount)
	}

	if s.Tables[0].Columns[0].Stats == nil {
		t.Error("Column stats should be applied")
	}

	if s.Tables[0].Columns[0].Stats.DistinctCount != 100 {
		t.Errorf("Expected distinct count 100, got %d", s.Tables[0].Columns[0].Stats.DistinctCount)
	}
}

func TestUpdateCheckpointWithColumnStats(t *testing.T) {
	cp := &Checkpoint{}

	stats := &schema.ColumnStats{RowCount: 50, DistinctCount: 25}
	UpdateCheckpointWithColumnStats(cp, "users", "id", stats)

	if cp.PartialResult == nil {
		t.Fatal("PartialResult should be created")
	}

	if cp.PartialResult["users"] == nil {
		t.Fatal("Table entry should be created")
	}

	if cp.PartialResult["users"].Columns["id"] == nil {
		t.Fatal("Column entry should be created")
	}

	if cp.PartialResult["users"].Columns["id"].Stats.RowCount != 50 {
		t.Errorf("Expected row count 50, got %d", cp.PartialResult["users"].Columns["id"].Stats.RowCount)
	}

	if cp.Progress.CurrentTable != "users" {
		t.Errorf("Expected current table 'users', got '%s'", cp.Progress.CurrentTable)
	}

	if !contains(cp.Progress.CompletedColumns, "id") {
		t.Error("id should be in completed columns")
	}
}

func TestMarkTableCompleted(t *testing.T) {
	cp := &Checkpoint{
		Progress: CheckpointProgress{
			CurrentTable:     "users",
			CompletedColumns: []string{"id", "name"},
		},
	}

	MarkTableCompleted(cp, "users")

	if !contains(cp.Progress.CompletedTables, "users") {
		t.Error("users should be in completed tables")
	}

	if cp.Progress.CurrentTable != "" {
		t.Error("CurrentTable should be cleared")
	}

	if cp.Progress.CompletedColumns != nil {
		t.Error("CompletedColumns should be cleared")
	}
}
