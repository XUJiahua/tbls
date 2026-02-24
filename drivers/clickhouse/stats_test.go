//go:build clickhouse

package clickhouse

import (
	"context"
	"math"
	"os"
	"testing"

	"github.com/k1LoW/tbls/drivers"
	"github.com/k1LoW/tbls/schema"
	"github.com/xo/dburl"
)

// findTable finds a table in the schema by name
func findTable(s *schema.Schema, name string) *schema.Table {
	for _, t := range s.Tables {
		if t.Name == name {
			return t
		}
	}
	return nil
}

// findColumn finds a column in a table by name
func findColumn(t *schema.Table, name string) *schema.Column {
	for _, c := range t.Columns {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// assertApprox checks that got is within tolerance of want
func assertApprox(t *testing.T, label string, got, want, tolerance float64) {
	t.Helper()
	if math.Abs(got-want) > tolerance {
		t.Errorf("%s: got %v, want %v (±%v)", label, got, want, tolerance)
	}
}

// assertInt64 checks that got == want for int64 values
func assertInt64(t *testing.T, label string, got, want int64) {
	t.Helper()
	if got != want {
		t.Errorf("%s: got %d, want %d", label, got, want)
	}
}

func TestCollectStats(t *testing.T) {
	dsn := os.Getenv("TBLS_TEST_CLICKHOUSE_DSN")
	if dsn == "" {
		t.Skip("TBLS_TEST_CLICKHOUSE_DSN not set")
	}

	db, err := dburl.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ch := New(db)

	s := &schema.Schema{
		Name: "testdb",
	}

	// First analyze to get tables (fixed: pass context)
	if err := ch.Analyze(context.Background(), s); err != nil {
		t.Fatal(err)
	}

	if findTable(s, "stats_basic") == nil {
		t.Skip("stats_basic table not found - rebuild ClickHouse container with 02_stats.sql")
	}

	cfg := drivers.StatsConfig{
		Include:    []string{"stats_basic", "stats_numbers"},
		TopN:       10,
		SampleSize: -1, // Full scan for deterministic results
		Ctx:        context.Background(),
	}

	if err := ch.CollectStats(s, cfg); err != nil {
		t.Fatal(err)
	}

	// --- stats_basic assertions ---
	basicTable := findTable(s, "stats_basic")
	if basicTable == nil {
		t.Fatal("stats_basic not found after stats collection")
	}
	if basicTable.Stats == nil {
		t.Fatal("stats_basic has no table stats")
	}
	assertInt64(t, "stats_basic.RowCount", basicTable.Stats.RowCount, 10000)

	// id: UInt64, 1..10000, no nulls
	idCol := findColumn(basicTable, "id")
	if idCol == nil || idCol.Stats == nil {
		t.Fatal("stats_basic.id column or stats not found")
	}
	assertInt64(t, "id.RowCount", idCol.Stats.RowCount, 10000)
	assertInt64(t, "id.NullCount", idCol.Stats.NullCount, 0)
	assertInt64(t, "id.DistinctCount", idCol.Stats.DistinctCount, 10000)
	if idCol.Stats.Min == nil || idCol.Stats.Max == nil || idCol.Stats.Avg == nil {
		t.Fatal("id should have numeric stats (Min/Max/Avg)")
	}
	assertApprox(t, "id.Min", *idCol.Stats.Min, 1.0, 0.01)
	assertApprox(t, "id.Max", *idCol.Stats.Max, 10000.0, 0.01)
	assertApprox(t, "id.Avg", *idCol.Stats.Avg, 5000.5, 0.5)

	// name: String, 10000 distinct, all length 10
	nameCol := findColumn(basicTable, "name")
	if nameCol == nil || nameCol.Stats == nil {
		t.Fatal("stats_basic.name column or stats not found")
	}
	assertInt64(t, "name.NullCount", nameCol.Stats.NullCount, 0)
	assertInt64(t, "name.DistinctCount", nameCol.Stats.DistinctCount, 10000)
	if nameCol.Stats.MinLength == nil || nameCol.Stats.MaxLength == nil {
		t.Fatal("name should have string length stats")
	}
	assertInt64(t, "name.MinLength", *nameCol.Stats.MinLength, 10)
	assertInt64(t, "name.MaxLength", *nameCol.Stats.MaxLength, 10)

	// email: Nullable(String), 10% null (i%10==0)
	emailCol := findColumn(basicTable, "email")
	if emailCol == nil || emailCol.Stats == nil {
		t.Fatal("stats_basic.email column or stats not found")
	}
	assertInt64(t, "email.NullCount", emailCol.Stats.NullCount, 1000)
	assertApprox(t, "email.NullPercent", emailCol.Stats.NullPercent, 10.0, 0.1)
	assertInt64(t, "email.DistinctCount", emailCol.Stats.DistinctCount, 9000)

	// age: Nullable(Int32), 15% null (i%100 < 15)
	ageCol := findColumn(basicTable, "age")
	if ageCol == nil || ageCol.Stats == nil {
		t.Fatal("stats_basic.age column or stats not found")
	}
	assertInt64(t, "age.NullCount", ageCol.Stats.NullCount, 1500)
	assertApprox(t, "age.NullPercent", ageCol.Stats.NullPercent, 15.0, 0.1)
	assertInt64(t, "age.DistinctCount", ageCol.Stats.DistinctCount, 63)
	if ageCol.Stats.Min == nil || ageCol.Stats.Max == nil {
		t.Fatal("age should have numeric stats")
	}
	assertApprox(t, "age.Min", *ageCol.Stats.Min, 18.0, 0.01)
	assertApprox(t, "age.Max", *ageCol.Stats.Max, 80.0, 0.01)

	// score: Float64, 0.0..99.9
	scoreCol := findColumn(basicTable, "score")
	if scoreCol == nil || scoreCol.Stats == nil {
		t.Fatal("stats_basic.score column or stats not found")
	}
	assertInt64(t, "score.NullCount", scoreCol.Stats.NullCount, 0)
	assertInt64(t, "score.DistinctCount", scoreCol.Stats.DistinctCount, 1000)
	if scoreCol.Stats.Min == nil || scoreCol.Stats.Max == nil {
		t.Fatal("score should have numeric stats")
	}
	assertApprox(t, "score.Min", *scoreCol.Stats.Min, 0.0, 0.01)
	assertApprox(t, "score.Max", *scoreCol.Stats.Max, 99.9, 0.01)
	assertApprox(t, "score.Avg", *scoreCol.Stats.Avg, 49.95, 0.5)

	// status: LowCardinality(String), 5 distinct, top-N each 2000
	statusCol := findColumn(basicTable, "status")
	if statusCol == nil || statusCol.Stats == nil {
		t.Fatal("stats_basic.status column or stats not found")
	}
	assertInt64(t, "status.NullCount", statusCol.Stats.NullCount, 0)
	assertInt64(t, "status.DistinctCount", statusCol.Stats.DistinctCount, 5)
	if len(statusCol.Stats.TopValues) != 5 {
		t.Errorf("status.TopValues: got %d values, want 5", len(statusCol.Stats.TopValues))
	}
	for _, tv := range statusCol.Stats.TopValues {
		assertInt64(t, "status.TopValues["+tv.Value+"].Count", tv.Count, 2000)
	}

	// category: String, 20 distinct
	catCol := findColumn(basicTable, "category")
	if catCol == nil || catCol.Stats == nil {
		t.Fatal("stats_basic.category column or stats not found")
	}
	assertInt64(t, "category.DistinctCount", catCol.Stats.DistinctCount, 20)
	if catCol.Stats.MinLength == nil || catCol.Stats.MaxLength == nil {
		t.Fatal("category should have string length stats")
	}
	assertInt64(t, "category.MinLength", *catCol.Stats.MinLength, 6)
	assertInt64(t, "category.MaxLength", *catCol.Stats.MaxLength, 6)

	// created_at: DateTime, no nulls
	createdCol := findColumn(basicTable, "created_at")
	if createdCol == nil || createdCol.Stats == nil {
		t.Fatal("stats_basic.created_at column or stats not found")
	}
	assertInt64(t, "created_at.NullCount", createdCol.Stats.NullCount, 0)
	assertInt64(t, "created_at.DistinctCount", createdCol.Stats.DistinctCount, 10000)
	if createdCol.Stats.MinDate == nil || createdCol.Stats.MaxDate == nil {
		t.Fatal("created_at should have date stats")
	}

	// updated_at: Nullable(DateTime), 20% null (i%5==0)
	updatedCol := findColumn(basicTable, "updated_at")
	if updatedCol == nil || updatedCol.Stats == nil {
		t.Fatal("stats_basic.updated_at column or stats not found")
	}
	assertInt64(t, "updated_at.NullCount", updatedCol.Stats.NullCount, 2000)
	assertApprox(t, "updated_at.NullPercent", updatedCol.Stats.NullPercent, 20.0, 0.1)

	// note: Nullable(String), 30% null (i%10 < 3)
	noteCol := findColumn(basicTable, "note")
	if noteCol == nil || noteCol.Stats == nil {
		t.Fatal("stats_basic.note column or stats not found")
	}
	assertInt64(t, "note.NullCount", noteCol.Stats.NullCount, 3000)
	assertApprox(t, "note.NullPercent", noteCol.Stats.NullPercent, 30.0, 0.1)

	// payload: String in ClickHouse → NOT complex, should have string stats
	payloadCol := findColumn(basicTable, "payload")
	if payloadCol == nil || payloadCol.Stats == nil {
		t.Fatal("stats_basic.payload column or stats not found")
	}
	if payloadCol.Stats.SkippedComplexAnalysis {
		t.Error("payload (String in CH) should NOT have SkippedComplexAnalysis=true")
	}

	// --- stats_numbers assertions ---
	numTable := findTable(s, "stats_numbers")
	if numTable == nil {
		t.Fatal("stats_numbers not found after stats collection")
	}
	if numTable.Stats == nil {
		t.Fatal("stats_numbers has no table stats")
	}
	assertInt64(t, "stats_numbers.RowCount", numTable.Stats.RowCount, 10000)

	// int_val: Int32, -500..499
	intCol := findColumn(numTable, "int_val")
	if intCol == nil || intCol.Stats == nil {
		t.Fatal("stats_numbers.int_val column or stats not found")
	}
	assertInt64(t, "int_val.NullCount", intCol.Stats.NullCount, 0)
	assertInt64(t, "int_val.DistinctCount", intCol.Stats.DistinctCount, 1000)
	if intCol.Stats.Min == nil || intCol.Stats.Max == nil || intCol.Stats.Avg == nil {
		t.Fatal("int_val should have numeric stats")
	}
	assertApprox(t, "int_val.Min", *intCol.Stats.Min, -500.0, 0.01)
	assertApprox(t, "int_val.Max", *intCol.Stats.Max, 499.0, 0.01)
	assertApprox(t, "int_val.Avg", *intCol.Stats.Avg, -0.5, 0.01)

	// float_val: Float64, 1.5..15000.0
	floatCol := findColumn(numTable, "float_val")
	if floatCol == nil || floatCol.Stats == nil {
		t.Fatal("stats_numbers.float_val column or stats not found")
	}
	assertInt64(t, "float_val.NullCount", floatCol.Stats.NullCount, 0)
	assertInt64(t, "float_val.DistinctCount", floatCol.Stats.DistinctCount, 10000)
	if floatCol.Stats.Min == nil || floatCol.Stats.Max == nil || floatCol.Stats.Avg == nil {
		t.Fatal("float_val should have numeric stats")
	}
	assertApprox(t, "float_val.Min", *floatCol.Stats.Min, 1.5, 0.01)
	assertApprox(t, "float_val.Max", *floatCol.Stats.Max, 15000.0, 0.01)
	assertApprox(t, "float_val.Avg", *floatCol.Stats.Avg, 7500.75, 0.5)

	// nullable_int: Nullable(Int32), 20% null (i%5==0)
	nullableIntCol := findColumn(numTable, "nullable_int")
	if nullableIntCol == nil || nullableIntCol.Stats == nil {
		t.Fatal("stats_numbers.nullable_int column or stats not found")
	}
	assertInt64(t, "nullable_int.NullCount", nullableIntCol.Stats.NullCount, 2000)
	assertApprox(t, "nullable_int.NullPercent", nullableIntCol.Stats.NullPercent, 20.0, 0.1)
	assertInt64(t, "nullable_int.DistinctCount", nullableIntCol.Stats.DistinctCount, 80)

	// small_distinct: UInt8, 10 distinct (0..9), each 1000
	smallCol := findColumn(numTable, "small_distinct")
	if smallCol == nil || smallCol.Stats == nil {
		t.Fatal("stats_numbers.small_distinct column or stats not found")
	}
	assertInt64(t, "small_distinct.NullCount", smallCol.Stats.NullCount, 0)
	assertInt64(t, "small_distinct.DistinctCount", smallCol.Stats.DistinctCount, 10)
	assertApprox(t, "small_distinct.Avg", *smallCol.Stats.Avg, 4.5, 0.01)
	if len(smallCol.Stats.TopValues) != 10 {
		t.Errorf("small_distinct.TopValues: got %d values, want 10", len(smallCol.Stats.TopValues))
	}
	for _, tv := range smallCol.Stats.TopValues {
		assertInt64(t, "small_distinct.TopValues["+tv.Value+"].Count", tv.Count, 1000)
	}
}
