//go:build clickhouse

package clickhouse

import (
	"database/sql"
	"os"
	"testing"

	"github.com/k1LoW/tbls/drivers"
	"github.com/k1LoW/tbls/schema"
)

func TestCollectStats(t *testing.T) {
	dsn := os.Getenv("TBLS_TEST_CLICKHOUSE_DSN")
	if dsn == "" {
		t.Skip("TBLS_TEST_CLICKHOUSE_DSN not set")
	}

	db, err := sql.Open("clickhouse", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ch := New(db)

	s := &schema.Schema{
		Name: "default",
	}

	// First analyze to get tables
	if err := ch.Analyze(s); err != nil {
		t.Fatal(err)
	}

	// Then collect stats
	cfg := drivers.StatsConfig{
		TopN:                10,
		SampleSize:          10000,
		LargeTableThreshold: 1000000,
		RecentDays:          30,
	}

	if err := ch.CollectStats(s, cfg); err != nil {
		t.Fatal(err)
	}

	// Sanity check: ensure we have tables to test
	if len(s.Tables) == 0 {
		t.Skip("no tables found in database")
	}

	// Verify table stats were collected
	for _, table := range s.Tables {
		if table.Stats == nil {
			t.Errorf("table %s has no stats", table.Name)
			continue
		}
		if table.Stats.RowCount < 0 {
			t.Errorf("table %s has invalid row count", table.Name)
		}
	}

	// Verify column stats were collected
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
}
