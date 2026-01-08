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
	}

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

	scaffolded, err := GenerateScaffoldConfig(c, s, nil)
	if err != nil {
		t.Fatalf("generateScaffoldConfig failed: %v", err)
	}

	// Verify DSN is preserved
	if scaffolded.DSN.URL != c.DSN.URL {
		t.Errorf("DSN.URL mismatch: got %s, want %s", scaffolded.DSN.URL, c.DSN.URL)
	}

	// Verify Stats defaults
	if scaffolded.Stats.TopN != 10 {
		t.Errorf("Stats.TopN mismatch: got %d, want 10", scaffolded.Stats.TopN)
	}
	if scaffolded.Stats.SampleSize != 10000 {
		t.Errorf("Stats.SampleSize mismatch: got %d, want 10000", scaffolded.Stats.SampleSize)
	}

	// Verify tables list
	if len(scaffolded.Stats.Tables) != 2 {
		t.Errorf("Stats.Tables count mismatch: got %d, want 2", len(scaffolded.Stats.Tables))
	}

	// Verify table names and mode are set correctly
	tableNames := make(map[string]bool)
	for _, table := range scaffolded.Stats.Tables {
		if table.Name == "" {
			t.Errorf("Table name should not be empty")
		}
		// Without detected date columns, should fall back to row_limit mode
		if table.Mode != config.SamplingModeRowLimit {
			t.Errorf("Expected mode=row_limit for %s, got %s", table.Name, table.Mode)
		}
		// Should have default sampleSize
		if table.SampleSize != 10000 {
			t.Errorf("Expected sampleSize=10000 for %s, got %d", table.Name, table.SampleSize)
		}
		tableNames[table.Name] = true
	}
	if !tableNames["users"] {
		t.Errorf("Expected table 'users' not found in Stats.Tables")
	}
	if !tableNames["posts"] {
		t.Errorf("Expected table 'posts' not found in Stats.Tables")
	}
}

func TestGenerateScaffoldConfigWithDateColumns(t *testing.T) {
	// Create a minimal config
	c := &config.Config{
		DSN: config.DSN{
			URL: "clickhouse://localhost:9000/testdb",
		},
	}

	// Create a minimal schema
	s := &schema.Schema{
		Name: "testdb",
		Tables: []*schema.Table{
			{Name: "events"},
			{Name: "logs"},
		},
	}

	// Simulate detected date columns
	detectedDateColumns := map[string]string{
		"events": "event_date",
		"logs":   "log_time",
	}

	scaffolded, err := GenerateScaffoldConfig(c, s, detectedDateColumns)
	if err != nil {
		t.Fatalf("generateScaffoldConfig failed: %v", err)
	}

	// Verify date columns and mode are set correctly
	for _, table := range scaffolded.Stats.Tables {
		switch table.Name {
		case "events":
			if table.Mode != config.SamplingModeDateFilter {
				t.Errorf("Expected events.mode=date_filter, got %s", table.Mode)
			}
			if table.DateColumn != "event_date" {
				t.Errorf("Expected events.dateColumn=event_date, got %s", table.DateColumn)
			}
		case "logs":
			if table.Mode != config.SamplingModeDateFilter {
				t.Errorf("Expected logs.mode=date_filter, got %s", table.Mode)
			}
			if table.DateColumn != "log_time" {
				t.Errorf("Expected logs.dateColumn=log_time, got %s", table.DateColumn)
			}
		}
	}
}

func TestGenerateScaffoldConfigWithUserOverride(t *testing.T) {
	// Create a config with user-specified table configs using explicit mode
	c := &config.Config{
		DSN: config.DSN{
			URL: "clickhouse://localhost:9000/testdb",
		},
		Stats: config.StatsConfig{
			Tables: []config.TableStatsConfig{
				{Name: "events", Mode: config.SamplingModeRowLimit, SampleSize: 5000},
			},
		},
	}

	s := &schema.Schema{
		Name: "testdb",
		Tables: []*schema.Table{
			{Name: "events"},
		},
	}

	// Simulate detected date column that should be overridden
	detectedDateColumns := map[string]string{
		"events": "event_date",
	}

	scaffolded, err := GenerateScaffoldConfig(c, s, detectedDateColumns)
	if err != nil {
		t.Fatalf("generateScaffoldConfig failed: %v", err)
	}

	// Verify user config takes precedence
	for _, table := range scaffolded.Stats.Tables {
		if table.Name == "events" {
			if table.Mode != config.SamplingModeRowLimit {
				t.Errorf("Expected events.mode=row_limit, got %s", table.Mode)
			}
			if table.SampleSize != 5000 {
				t.Errorf("Expected events.sampleSize=5000, got %d", table.SampleSize)
			}
		}
	}
}

func TestGenerateScaffoldConfigRowLimitFallback(t *testing.T) {
	// Create a minimal config with global sampleSize
	c := &config.Config{
		DSN: config.DSN{
			URL: "postgres://localhost:5432/testdb",
		},
		Stats: config.StatsConfig{
			SampleSize: 20000,
		},
	}

	s := &schema.Schema{
		Name: "testdb",
		Tables: []*schema.Table{
			{Name: "users"},
		},
	}

	// No detected date columns
	scaffolded, err := GenerateScaffoldConfig(c, s, nil)
	if err != nil {
		t.Fatalf("generateScaffoldConfig failed: %v", err)
	}

	// Verify row_limit mode is used with global sampleSize
	for _, table := range scaffolded.Stats.Tables {
		if table.Name == "users" {
			if table.Mode != config.SamplingModeRowLimit {
				t.Errorf("Expected users.mode=row_limit, got %s", table.Mode)
			}
			if table.SampleSize != 20000 {
				t.Errorf("Expected users.sampleSize=20000, got %d", table.SampleSize)
			}
		}
	}
}
