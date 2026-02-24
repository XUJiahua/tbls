package datasource

import (
	"database/sql"
	"testing"

	"github.com/k1LoW/tbls/config"
	"github.com/k1LoW/tbls/schema"
	_ "github.com/lib/pq"
)

// findTableByName finds a table in the schema by name
func findTableByName(s *schema.Schema, name string) *schema.Table {
	for _, t := range s.Tables {
		if t.Name == name {
			return t
		}
	}
	return nil
}

func TestAnalyzeWithStatsPostgres(t *testing.T) {
	dsn := "pg://postgres:pgpass@localhost:55413/testdb?sslmode=disable"

	// Check connectivity first
	db, err := sql.Open("postgres", "postgres://postgres:pgpass@localhost:55413/testdb?sslmode=disable")
	if err != nil {
		t.Skip("cannot connect to PostgreSQL:", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		t.Skip("PostgreSQL not reachable:", err)
	}
	db.Close()

	cfg := &config.Config{
		Stats: config.StatsConfig{
			Enabled:    true,
			TopN:       10,
			SampleSize: -1,
			Include:    []string{"public.stats_basic", "public.stats_numbers"},
		},
	}

	s, err := AnalyzeWithStats(config.DSN{URL: dsn}, cfg)
	if err != nil {
		t.Fatal(err)
	}

	// Verify stats_basic
	basicTable := findTableByName(s, "public.stats_basic")
	if basicTable == nil {
		t.Fatal("public.stats_basic not found")
	}
	if basicTable.Stats == nil {
		t.Fatal("public.stats_basic has no table stats")
	}
	if basicTable.Stats.RowCount != 10000 {
		t.Errorf("stats_basic.RowCount: got %d, want 10000", basicTable.Stats.RowCount)
	}

	// Verify columns have stats
	for _, col := range basicTable.Columns {
		if col.Stats == nil {
			t.Errorf("public.stats_basic.%s has no column stats", col.Name)
		}
	}

	// Verify stats_numbers
	numTable := findTableByName(s, "public.stats_numbers")
	if numTable == nil {
		t.Fatal("public.stats_numbers not found")
	}
	if numTable.Stats == nil {
		t.Fatal("public.stats_numbers has no table stats")
	}
	if numTable.Stats.RowCount != 10000 {
		t.Errorf("stats_numbers.RowCount: got %d, want 10000", numTable.Stats.RowCount)
	}

	// Verify only the included tables have stats (others should not)
	for _, table := range s.Tables {
		if table.Name != "public.stats_basic" && table.Name != "public.stats_numbers" {
			for _, col := range table.Columns {
				if col.Stats != nil {
					t.Errorf("table %s.%s should not have stats (not in include list)", table.Name, col.Name)
				}
			}
		}
	}
}
