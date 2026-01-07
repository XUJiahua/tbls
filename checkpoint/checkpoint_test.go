package checkpoint

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetCheckpointDir(t *testing.T) {
	dir := GetCheckpointDir()
	expected := filepath.Join(os.TempDir(), CheckpointDirName)
	if dir != expected {
		t.Errorf("Expected %s, got %s", expected, dir)
	}
}

func TestHashDSNURL(t *testing.T) {
	hash1 := HashDSNURL("postgres://user:pass@localhost/db")
	hash2 := HashDSNURL("postgres://user:pass@localhost/db")
	hash3 := HashDSNURL("postgres://user:pass@localhost/other")

	if hash1 != hash2 {
		t.Error("Same DSN URL should produce same hash")
	}

	if hash1 == hash3 {
		t.Error("Different DSN URL should produce different hash")
	}

	// Hash should be 64 characters (SHA256 hex)
	if len(hash1) != 64 {
		t.Errorf("Expected hash length 64, got %d", len(hash1))
	}
}

func TestGetCheckpointPath(t *testing.T) {
	dsnURL := "postgres://test@localhost/testdb"
	path := GetCheckpointPath(dsnURL)

	// Should be in temp dir
	if !filepath.HasPrefix(path, os.TempDir()) {
		t.Errorf("Path should be in temp dir, got %s", path)
	}

	// Should end with .json
	if filepath.Ext(path) != CheckpointExtension {
		t.Errorf("Path should end with %s, got %s", CheckpointExtension, filepath.Ext(path))
	}

	// Same DSN should produce same path
	path2 := GetCheckpointPath(dsnURL)
	if path != path2 {
		t.Error("Same DSN should produce same path")
	}
}

func TestCheckpointExistsAndDelete(t *testing.T) {
	dsnURL := "postgres://test-checkpoint-exists@localhost/testdb"

	// Clean up first
	DeleteCheckpoint(dsnURL)

	// Should not exist initially
	if CheckpointExists(dsnURL) {
		t.Error("Checkpoint should not exist initially")
	}

	// Create a checkpoint file
	EnsureCheckpointDir()
	path := GetCheckpointPath(dsnURL)
	if err := os.WriteFile(path, []byte("{}"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Should exist now
	if !CheckpointExists(dsnURL) {
		t.Error("Checkpoint should exist after creation")
	}

	// Delete it
	if err := DeleteCheckpoint(dsnURL); err != nil {
		t.Fatalf("Failed to delete checkpoint: %v", err)
	}

	// Should not exist anymore
	if CheckpointExists(dsnURL) {
		t.Error("Checkpoint should not exist after deletion")
	}

	// Deleting non-existent checkpoint should not error
	if err := DeleteCheckpoint(dsnURL); err != nil {
		t.Errorf("Deleting non-existent checkpoint should not error: %v", err)
	}
}
