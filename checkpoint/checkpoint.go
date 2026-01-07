// Package checkpoint provides DSN-based checkpoint path management for tbls.
// Checkpoints are stored in the system temp directory, addressed by DSN URL hash.
package checkpoint

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
)

const (
	// CheckpointDirName is the directory name for checkpoint files
	CheckpointDirName = "tbls-checkpoints"
	// CheckpointExtension is the file extension for checkpoint files
	CheckpointExtension = ".json"
)

// GetCheckpointDir returns the directory where checkpoint files are stored.
// Path: {os.TempDir()}/tbls-checkpoints/
func GetCheckpointDir() string {
	return filepath.Join(os.TempDir(), CheckpointDirName)
}

// GetCheckpointPath returns the full path to a checkpoint file for the given DSN URL.
// Path: {os.TempDir()}/tbls-checkpoints/{sha256(dsnURL)}.json
func GetCheckpointPath(dsnURL string) string {
	hash := HashDSNURL(dsnURL)
	return filepath.Join(GetCheckpointDir(), hash+CheckpointExtension)
}

// HashDSNURL creates a SHA256 hash of the DSN URL for checkpoint file naming.
// Only the URL is hashed, headers are ignored.
func HashDSNURL(dsnURL string) string {
	h := sha256.Sum256([]byte(dsnURL))
	return hex.EncodeToString(h[:])
}

// EnsureCheckpointDir creates the checkpoint directory if it doesn't exist.
func EnsureCheckpointDir() error {
	return os.MkdirAll(GetCheckpointDir(), 0755)
}

// DeleteCheckpoint removes the checkpoint file for the given DSN URL.
// Returns nil if the file doesn't exist.
func DeleteCheckpoint(dsnURL string) error {
	path := GetCheckpointPath(dsnURL)
	err := os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// CheckpointExists returns true if a checkpoint file exists for the given DSN URL.
func CheckpointExists(dsnURL string) bool {
	path := GetCheckpointPath(dsnURL)
	_, err := os.Stat(path)
	return err == nil
}
