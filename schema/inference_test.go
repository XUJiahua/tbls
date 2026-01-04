package schema

import (
	"testing"
)

func TestInferPrimaryKey(t *testing.T) {
	tests := []struct {
		name         string
		stats        *ColumnStats
		rowCount     int64
		wantPK       bool
		wantUnique   bool
	}{
		{
			name: "primary key - all unique no nulls",
			stats: &ColumnStats{
				RowCount:      100,
				DistinctCount: 100,
				NullCount:     0,
			},
			rowCount:   100,
			wantPK:     true,
			wantUnique: false,
		},
		{
			name: "unique with nulls",
			stats: &ColumnStats{
				RowCount:      100,
				DistinctCount: 100,
				NullCount:     5,
			},
			rowCount:   100,
			wantPK:     false,
			wantUnique: true,
		},
		{
			name: "not unique",
			stats: &ColumnStats{
				RowCount:      100,
				DistinctCount: 50,
				NullCount:     0,
			},
			rowCount:   100,
			wantPK:     false,
			wantUnique: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			col := &Column{
				Name:  "test_col",
				Stats: tt.stats,
			}
			col.Inferences = &ColumnInferences{}

			opts := &InferenceOptions{
				EnumMaxCardinality:      0.01,
				EnumMaxDistinct:         20,
				DictMaxCardinality:      0.05,
				DictMaxDistinct:         100,
				ForeignKeyMinConfidence: 0.7,
			}
			inferrer := NewInferrer(opts)
			inferrer.inferPrimaryKey(col, tt.rowCount)

			if col.Inferences.IsPrimaryKey != tt.wantPK {
				t.Errorf("IsPrimaryKey = %v, want %v", col.Inferences.IsPrimaryKey, tt.wantPK)
			}
			if col.Inferences.IsUnique != tt.wantUnique {
				t.Errorf("IsUnique = %v, want %v", col.Inferences.IsUnique, tt.wantUnique)
			}
		})
	}
}

func TestInferEnum(t *testing.T) {
	tests := []struct {
		name        string
		stats       *ColumnStats
		rowCount    int64
		wantIsEnum  bool
		wantType    string
	}{
		{
			name: "enum - very low cardinality",
			stats: &ColumnStats{
				RowCount:      10000,
				DistinctCount: 5,
			},
			rowCount:   10000,
			wantIsEnum: true,
			wantType:   "enum",
		},
		{
			name: "dictionary - medium cardinality",
			stats: &ColumnStats{
				RowCount:      10000,
				DistinctCount: 50,
			},
			rowCount:   10000,
			wantIsEnum: true,
			wantType:   "dictionary",
		},
		{
			name: "high cardinality",
			stats: &ColumnStats{
				RowCount:      10000,
				DistinctCount: 5000,
			},
			rowCount:   10000,
			wantIsEnum: false,
			wantType:   "high_cardinality",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			col := &Column{
				Name:  "test_col",
				Stats: tt.stats,
			}
			col.Inferences = &ColumnInferences{}

			opts := &InferenceOptions{
				EnumMaxCardinality:      0.01,
				EnumMaxDistinct:         20,
				DictMaxCardinality:      0.05,
				DictMaxDistinct:         100,
				ForeignKeyMinConfidence: 0.7,
			}
			inferrer := NewInferrer(opts)
			inferrer.inferEnum(col, tt.rowCount)

			if col.Inferences.IsEnum != tt.wantIsEnum {
				t.Errorf("IsEnum = %v, want %v", col.Inferences.IsEnum, tt.wantIsEnum)
			}
			if col.Inferences.EnumType != tt.wantType {
				t.Errorf("EnumType = %v, want %v", col.Inferences.EnumType, tt.wantType)
			}
		})
	}
}

func TestInferDataQuality(t *testing.T) {
	tests := []struct {
		name         string
		stats        *ColumnStats
		wantHasNulls bool
		wantComplete bool
	}{
		{
			name: "complete - no nulls",
			stats: &ColumnStats{
				NullCount: 0,
			},
			wantHasNulls: false,
			wantComplete: true,
		},
		{
			name: "has nulls",
			stats: &ColumnStats{
				NullCount: 10,
			},
			wantHasNulls: true,
			wantComplete: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			col := &Column{
				Name:  "test_col",
				Stats: tt.stats,
			}
			col.Inferences = &ColumnInferences{}

			opts := &InferenceOptions{}
			inferrer := NewInferrer(opts)
			inferrer.inferDataQuality(col)

			if col.Inferences.HasNulls != tt.wantHasNulls {
				t.Errorf("HasNulls = %v, want %v", col.Inferences.HasNulls, tt.wantHasNulls)
			}
			if col.Inferences.IsComplete != tt.wantComplete {
				t.Errorf("IsComplete = %v, want %v", col.Inferences.IsComplete, tt.wantComplete)
			}
		})
	}
}

func TestInferDistribution(t *testing.T) {
	col := &Column{
		Name: "status",
		Stats: &ColumnStats{
			RowCount: 100,
			TopValues: []TopValue{
				{Value: "active", Count: 60},
				{Value: "inactive", Count: 30},
				{Value: "pending", Count: 10},
			},
		},
	}
	col.Inferences = &ColumnInferences{}

	opts := &InferenceOptions{}
	inferrer := NewInferrer(opts)
	inferrer.inferDistribution(col, 100)

	if len(col.Inferences.Distribution) != 3 {
		t.Fatalf("Distribution length = %d, want 3", len(col.Inferences.Distribution))
	}

	expected := []struct {
		value   string
		percent float64
	}{
		{"active", 60.0},
		{"inactive", 30.0},
		{"pending", 10.0},
	}

	for i, exp := range expected {
		if col.Inferences.Distribution[i].Value != exp.value {
			t.Errorf("Distribution[%d].Value = %v, want %v", i, col.Inferences.Distribution[i].Value, exp.value)
		}
		if col.Inferences.Distribution[i].Percent != exp.percent {
			t.Errorf("Distribution[%d].Percent = %v, want %v", i, col.Inferences.Distribution[i].Percent, exp.percent)
		}
	}
}

func TestInferForeignKeys(t *testing.T) {
	// Create test schema with customers and orders tables
	customersTable := &Table{
		Name: "customers",
		Columns: []*Column{
			{
				Name: "id",
				PK:   true,
				Stats: &ColumnStats{
					RowCount:      100,
					DistinctCount: 100,
					Min:           float64Ptr(1),
					Max:           float64Ptr(100),
				},
			},
			{
				Name: "name",
				Stats: &ColumnStats{
					RowCount:      100,
					DistinctCount: 100,
				},
			},
		},
		Stats: &TableStats{RowCount: 100},
	}

	ordersTable := &Table{
		Name: "orders",
		Columns: []*Column{
			{
				Name: "id",
				PK:   true,
				Stats: &ColumnStats{
					RowCount:      500,
					DistinctCount: 500,
				},
			},
			{
				Name: "customer_id",
				Stats: &ColumnStats{
					RowCount:      500,
					DistinctCount: 80,
					Min:           float64Ptr(1),
					Max:           float64Ptr(100),
				},
			},
		},
		Stats: &TableStats{RowCount: 500},
	}

	schema := &Schema{
		Tables: []*Table{customersTable, ordersTable},
	}

	opts := &InferenceOptions{
		EnumMaxCardinality:      0.01,
		EnumMaxDistinct:         20,
		DictMaxCardinality:      0.05,
		DictMaxDistinct:         100,
		ForeignKeyMinConfidence: 0.7,
	}
	inferrer := NewInferrer(opts)
	fks := inferrer.inferForeignKeys(schema)

	if len(fks) != 1 {
		t.Fatalf("ForeignKeys length = %d, want 1", len(fks))
	}

	fk := fks[0]
	if fk.SourceTable != "orders" {
		t.Errorf("SourceTable = %v, want orders", fk.SourceTable)
	}
	if fk.SourceColumn != "customer_id" {
		t.Errorf("SourceColumn = %v, want customer_id", fk.SourceColumn)
	}
	if fk.TargetTable != "customers" {
		t.Errorf("TargetTable = %v, want customers", fk.TargetTable)
	}
	if fk.TargetColumn != "id" {
		t.Errorf("TargetColumn = %v, want id", fk.TargetColumn)
	}
	if fk.Confidence < 0.7 {
		t.Errorf("Confidence = %v, want >= 0.7", fk.Confidence)
	}
}

func TestInferBusinessInsights(t *testing.T) {
	// Create test schema
	customersTable := &Table{
		Name: "customers",
		Columns: []*Column{
			{Name: "id", PK: true},
		},
		Stats: &TableStats{RowCount: 100},
	}

	ordersTable := &Table{
		Name: "orders",
		Columns: []*Column{
			{Name: "id", PK: true},
			{
				Name: "customer_id",
				Stats: &ColumnStats{
					DistinctCount: 80,
				},
			},
		},
		Stats: &TableStats{RowCount: 500},
	}

	schema := &Schema{
		Tables: []*Table{customersTable, ordersTable},
		Inferences: &SchemaInferences{
			ForeignKeys: []InferredForeignKey{
				{
					SourceTable:  "orders",
					SourceColumn: "customer_id",
					TargetTable:  "customers",
					TargetColumn: "id",
					Confidence:   1.0,
				},
			},
		},
	}

	opts := &InferenceOptions{}
	inferrer := NewInferrer(opts)
	insights := inferrer.inferBusinessInsights(schema)

	// Should have 2 insights: avg_per_entity and entity_coverage
	if len(insights) != 2 {
		t.Fatalf("BusinessInsights length = %d, want 2", len(insights))
	}

	// Check avg_per_entity
	var avgInsight *BusinessInsight
	var coverageInsight *BusinessInsight
	for i := range insights {
		if insights[i].Type == "avg_per_entity" {
			avgInsight = &insights[i]
		}
		if insights[i].Type == "entity_coverage" {
			coverageInsight = &insights[i]
		}
	}

	if avgInsight == nil {
		t.Fatal("avg_per_entity insight not found")
	}
	if avgInsight.Value != 5.0 {
		t.Errorf("avg_per_entity value = %v, want 5.0", avgInsight.Value)
	}

	if coverageInsight == nil {
		t.Fatal("entity_coverage insight not found")
	}
	if coverageInsight.Value != 0.8 {
		t.Errorf("entity_coverage value = %v, want 0.8", coverageInsight.Value)
	}
}

func TestRunInference(t *testing.T) {
	// Create a complete test schema
	customersTable := &Table{
		Name: "customers",
		Columns: []*Column{
			{
				Name: "id",
				PK:   true,
				Stats: &ColumnStats{
					RowCount:      100,
					DistinctCount: 100,
					NullCount:     0,
					Min:           float64Ptr(1),
					Max:           float64Ptr(100),
				},
			},
			{
				Name: "status",
				Stats: &ColumnStats{
					RowCount:      100,
					DistinctCount: 3,
					NullCount:     0,
					TopValues: []TopValue{
						{Value: "active", Count: 60},
						{Value: "inactive", Count: 30},
						{Value: "pending", Count: 10},
					},
				},
			},
		},
		Stats: &TableStats{RowCount: 100},
	}

	ordersTable := &Table{
		Name: "orders",
		Columns: []*Column{
			{
				Name: "id",
				PK:   true,
				Stats: &ColumnStats{
					RowCount:      500,
					DistinctCount: 500,
					NullCount:     0,
				},
			},
			{
				Name: "customer_id",
				Stats: &ColumnStats{
					RowCount:      500,
					DistinctCount: 80,
					NullCount:     0,
					Min:           float64Ptr(1),
					Max:           float64Ptr(100),
				},
			},
		},
		Stats: &TableStats{RowCount: 500},
	}

	schema := &Schema{
		Tables: []*Table{customersTable, ordersTable},
	}

	opts := &InferenceOptions{
		EnumMaxCardinality:      0.05,
		EnumMaxDistinct:         20,
		DictMaxCardinality:      0.1,
		DictMaxDistinct:         100,
		ForeignKeyMinConfidence: 0.7,
	}
	inferrer := NewInferrer(opts)
	err := inferrer.RunInference(schema)
	if err != nil {
		t.Fatalf("RunInference failed: %v", err)
	}

	// Check column-level inferences
	idCol := customersTable.Columns[0]
	if idCol.Inferences == nil {
		t.Fatal("customers.id inferences is nil")
	}
	if !idCol.Inferences.IsPrimaryKey {
		t.Error("customers.id should be inferred as primary key")
	}

	statusCol := customersTable.Columns[1]
	if statusCol.Inferences == nil {
		t.Fatal("customers.status inferences is nil")
	}
	if !statusCol.Inferences.IsEnum {
		t.Error("customers.status should be inferred as enum")
	}
	if statusCol.Inferences.EnumType != "enum" {
		t.Errorf("customers.status enum_type = %v, want enum", statusCol.Inferences.EnumType)
	}

	// Check schema-level inferences
	if schema.Inferences == nil {
		t.Fatal("schema inferences is nil")
	}
	if len(schema.Inferences.ForeignKeys) != 1 {
		t.Errorf("ForeignKeys length = %d, want 1", len(schema.Inferences.ForeignKeys))
	}
	if len(schema.Inferences.BusinessInsights) != 2 {
		t.Errorf("BusinessInsights length = %d, want 2", len(schema.Inferences.BusinessInsights))
	}
}

func float64Ptr(v float64) *float64 {
	return &v
}
