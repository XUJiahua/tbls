//go:build postgres

package postgres

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/k1LoW/tbls/drivers"
	"github.com/k1LoW/tbls/schema"
	_ "github.com/lib/pq"
)

func TestCollectStats(t *testing.T) {
	dsn := os.Getenv("TBLS_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TBLS_TEST_POSTGRES_DSN not set")
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	p := New(db)

	s := &schema.Schema{
		Name: "testdb",
	}

	// First analyze to get tables
	if err := p.Analyze(context.Background(), s); err != nil {
		t.Fatal(err)
	}

	if len(s.Tables) == 0 {
		t.Skip("no tables found in database")
	}

	// Test Mode A: direct queries
	t.Run("DirectQueries", func(t *testing.T) {
		cfg := drivers.StatsConfig{
			TopN:                10,
			SampleSize:          10000,
			LargeTableThreshold: 1000000,
			RecentDays:          30,
			UsePgStats:          false,
			Ctx:                 context.Background(),
		}

		if err := p.CollectStats(s, cfg); err != nil {
			t.Fatal(err)
		}

		for _, table := range s.Tables {
			if table.Stats == nil {
				t.Errorf("table %s has no stats", table.Name)
				continue
			}
			if table.Stats.RowCount < 0 {
				t.Errorf("table %s has invalid row count: %d", table.Name, table.Stats.RowCount)
			}
		}

		for _, table := range s.Tables {
			for _, col := range table.Columns {
				if col.Stats == nil {
					t.Errorf("column %s.%s has no stats", table.Name, col.Name)
					continue
				}
				if col.Stats.RowCount < 0 {
					t.Errorf("column %s.%s has invalid row count", table.Name, col.Name)
				}
			}
		}
	})

	// Test Mode B: pg_stats
	t.Run("PgStats", func(t *testing.T) {
		// Re-analyze to get fresh schema
		s2 := &schema.Schema{Name: "testdb"}
		if err := p.Analyze(context.Background(), s2); err != nil {
			t.Fatal(err)
		}

		cfg := drivers.StatsConfig{
			TopN:                10,
			SampleSize:          10000,
			LargeTableThreshold: 1000000,
			RecentDays:          30,
			UsePgStats:          true,
			Ctx:                 context.Background(),
		}

		if err := p.CollectStats(s2, cfg); err != nil {
			t.Fatal(err)
		}

		for _, table := range s2.Tables {
			if table.Stats == nil {
				t.Errorf("table %s has no stats", table.Name)
				continue
			}
		}

		for _, table := range s2.Tables {
			for _, col := range table.Columns {
				if col.Stats == nil {
					t.Errorf("column %s.%s has no stats", table.Name, col.Name)
					continue
				}
			}
		}
	})
}

func TestDetectDateColumns(t *testing.T) {
	dsn := os.Getenv("TBLS_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TBLS_TEST_POSTGRES_DSN not set")
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	p := New(db)

	s := &schema.Schema{
		Name: "testdb",
	}

	if err := p.Analyze(context.Background(), s); err != nil {
		t.Fatal(err)
	}

	if len(s.Tables) == 0 {
		t.Skip("no tables found in database")
	}

	result, err := p.DetectDateColumns(s)
	if err != nil {
		t.Fatal(err)
	}

	// Just verify the function runs without error and returns a map
	t.Logf("detected date columns: %v", result)
}
