package cmd

import (
	"testing"

	"github.com/k1LoW/tbls/config"
	"github.com/k1LoW/tbls/schema"
)

func TestGenerateScaffoldConfig(t *testing.T) {
	// Create a minimal config
	c := &config.Config{
		DSN: config.DSN{
			URL: "postgres://user:pass@localhost:5432/testdb",
		},
		DocPath: "dbdoc",
		ER: config.ER{
			Format: "svg",
		},
	}
	c.ER.Distance = &config.DefaultERDistance

	// Create a minimal schema
	s := &schema.Schema{
		Name: "testdb",
		Tables: []*schema.Table{
			{
				Name:    "users",
				Comment: "Users table",
				Columns: []*schema.Column{
					{Name: "id", Comment: "Primary key"},
					{Name: "name", Comment: ""},
					{Name: "email", Comment: "User email"},
				},
				Indexes: []*schema.Index{
					{Name: "users_pkey", Comment: ""},
				},
			},
			{
				Name:    "posts",
				Comment: "",
				Columns: []*schema.Column{
					{Name: "id", Comment: ""},
					{Name: "user_id", Comment: ""},
					{Name: "title", Comment: "Post title"},
				},
			},
		},
	}

	// Add users table reference for relation inference
	usersTable := s.Tables[0]
	usersTable.Columns[0].PK = true

	scaffolded, err := GenerateScaffoldConfig(c, s)
	if err != nil {
		t.Fatalf("generateScaffoldConfig failed: %v", err)
	}

	// Verify DSN is preserved
	if scaffolded.DSN.URL != c.DSN.URL {
		t.Errorf("DSN.URL mismatch: got %s, want %s", scaffolded.DSN.URL, c.DSN.URL)
	}

	// Verify DocPath is preserved
	if scaffolded.DocPath != c.DocPath {
		t.Errorf("DocPath mismatch: got %s, want %s", scaffolded.DocPath, c.DocPath)
	}

	// Verify ER settings
	if scaffolded.ER.Format != "svg" {
		t.Errorf("ER.Format mismatch: got %s, want svg", scaffolded.ER.Format)
	}
	if scaffolded.ER.Distance != 1 {
		t.Errorf("ER.Distance mismatch: got %d, want 1", scaffolded.ER.Distance)
	}

	// Verify Stats defaults
	if scaffolded.Stats.TopN != 10 {
		t.Errorf("Stats.TopN mismatch: got %d, want 10", scaffolded.Stats.TopN)
	}
	if scaffolded.Stats.SampleSize != 10000 {
		t.Errorf("Stats.SampleSize mismatch: got %d, want 10000", scaffolded.Stats.SampleSize)
	}

	// Verify tables map
	if len(scaffolded.Stats.Tables) != 2 {
		t.Errorf("Stats.Tables count mismatch: got %d, want 2", len(scaffolded.Stats.Tables))
	}
}
