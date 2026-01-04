package schema

import (
	"fmt"
	"regexp"
	"strings"
)

// InferenceOptions holds configuration for inference (mirrors config.InferenceConfig)
type InferenceOptions struct {
	EnumMaxCardinality      float64
	EnumMaxDistinct         int
	DictMaxCardinality      float64
	DictMaxDistinct         int
	ForeignKeyMinConfidence float64
}

// Inferrer performs statistics-based inference on schema
type Inferrer struct {
	options *InferenceOptions
}

// NewInferrer creates a new Inferrer with the given configuration
func NewInferrer(opts *InferenceOptions) *Inferrer {
	return &Inferrer{options: opts}
}

// RunInference executes all inference logic on the schema
func (i *Inferrer) RunInference(s *Schema) error {
	// 1. Column-level inferences (independent, could be parallelized)
	for _, table := range s.Tables {
		for _, col := range table.Columns {
			if col.Stats == nil {
				continue
			}
			i.inferColumnLevel(col, table.Stats)
		}
	}

	// 2. Cross-table inferences
	s.Inferences = &SchemaInferences{}

	// Foreign key inference
	s.Inferences.ForeignKeys = i.inferForeignKeys(s)

	// Business insights (depends on foreign key inference)
	s.Inferences.BusinessInsights = i.inferBusinessInsights(s)

	// Clean up empty inferences
	if len(s.Inferences.ForeignKeys) == 0 && len(s.Inferences.BusinessInsights) == 0 {
		s.Inferences = nil
	}

	return nil
}

// inferColumnLevel performs all column-level inferences
func (i *Inferrer) inferColumnLevel(col *Column, tableStats *TableStats) {
	col.Inferences = &ColumnInferences{}

	stats := col.Stats
	rowCount := stats.RowCount
	if tableStats != nil && tableStats.RowCount > 0 {
		rowCount = tableStats.RowCount
	}

	// Primary key / unique identifier detection
	i.inferPrimaryKey(col, rowCount)

	// Enum / dictionary detection
	i.inferEnum(col, rowCount)

	// Data quality
	i.inferDataQuality(col)

	// Data distribution
	i.inferDistribution(col, rowCount)
}

// inferPrimaryKey detects if a column is likely a primary key or unique column
func (i *Inferrer) inferPrimaryKey(col *Column, rowCount int64) {
	stats := col.Stats
	if rowCount == 0 || stats.DistinctCount == 0 {
		return
	}

	// If distinct count equals row count, it's unique
	if stats.DistinctCount == rowCount {
		if stats.NullCount == 0 {
			// No nulls and all distinct -> likely primary key
			col.Inferences.IsPrimaryKey = true
		} else {
			// Has nulls but all non-null values are distinct -> unique constraint
			col.Inferences.IsUnique = true
		}
	}
}

// inferEnum detects if a column is an enum or dictionary column
func (i *Inferrer) inferEnum(col *Column, rowCount int64) {
	stats := col.Stats
	if rowCount == 0 || stats.DistinctCount == 0 {
		col.Inferences.EnumType = "high_cardinality"
		return
	}

	cardinality := float64(stats.DistinctCount) / float64(rowCount)
	col.Inferences.Cardinality = cardinality

	cfg := i.options

	// Check for enum (low cardinality, few distinct values)
	if cardinality <= cfg.EnumMaxCardinality && stats.DistinctCount <= int64(cfg.EnumMaxDistinct) {
		col.Inferences.IsEnum = true
		col.Inferences.EnumType = "enum"
		return
	}

	// Check for dictionary (medium cardinality)
	if cardinality <= cfg.DictMaxCardinality && stats.DistinctCount <= int64(cfg.DictMaxDistinct) {
		col.Inferences.IsEnum = true
		col.Inferences.EnumType = "dictionary"
		return
	}

	col.Inferences.EnumType = "high_cardinality"
}

// inferDataQuality detects data quality issues
func (i *Inferrer) inferDataQuality(col *Column) {
	stats := col.Stats

	if stats.NullCount > 0 {
		col.Inferences.HasNulls = true
	}

	if stats.NullCount == 0 {
		col.Inferences.IsComplete = true
	}
}

// inferDistribution converts top values to distribution with percentages
func (i *Inferrer) inferDistribution(col *Column, rowCount int64) {
	stats := col.Stats
	if len(stats.TopValues) == 0 || rowCount == 0 {
		return
	}

	distribution := make([]DistributionItem, 0, len(stats.TopValues))
	for _, tv := range stats.TopValues {
		percent := float64(tv.Count) / float64(rowCount) * 100
		distribution = append(distribution, DistributionItem{
			Value:   tv.Value,
			Count:   tv.Count,
			Percent: percent,
		})
	}
	col.Inferences.Distribution = distribution
}

// inferForeignKeys detects potential foreign key relationships
func (i *Inferrer) inferForeignKeys(s *Schema) []InferredForeignKey {
	var fks []InferredForeignKey

	// Build a map of table name -> table for quick lookup
	tableMap := make(map[string]*Table)
	for _, t := range s.Tables {
		tableMap[t.Name] = t
		tableMap[strings.ToLower(t.Name)] = t
	}

	// Pattern for foreign key column names: xxx_id, xxxId, xxx_ID
	fkPattern := regexp.MustCompile(`^(.+?)(_id|Id|_ID|ID)$`)

	for _, table := range s.Tables {
		for _, col := range table.Columns {
			if col.Stats == nil {
				continue
			}

			// Check if column name matches FK pattern
			matches := fkPattern.FindStringSubmatch(col.Name)
			if matches == nil {
				continue
			}

			baseName := matches[1]

			// Try to find target table
			targetTable := i.findTargetTable(tableMap, baseName)
			if targetTable == nil || targetTable.Name == table.Name {
				continue
			}

			// Try to find target column (primary key of target table)
			targetColumn := i.findPrimaryKeyColumn(targetTable)
			if targetColumn == nil {
				continue
			}

			// Calculate confidence
			confidence := i.calculateFKConfidence(col, targetColumn)
			if confidence < i.options.ForeignKeyMinConfidence {
				continue
			}

			fks = append(fks, InferredForeignKey{
				SourceTable:  table.Name,
				SourceColumn: col.Name,
				TargetTable:  targetTable.Name,
				TargetColumn: targetColumn.Name,
				Confidence:   confidence,
			})
		}
	}

	return fks
}

// findTargetTable finds a table that matches the base name
func (i *Inferrer) findTargetTable(tableMap map[string]*Table, baseName string) *Table {
	// Try exact match
	if t, ok := tableMap[baseName]; ok {
		return t
	}

	// Try lowercase
	if t, ok := tableMap[strings.ToLower(baseName)]; ok {
		return t
	}

	// Try plural forms
	plurals := []string{
		baseName + "s",
		baseName + "es",
		strings.ToLower(baseName) + "s",
		strings.ToLower(baseName) + "es",
	}
	for _, p := range plurals {
		if t, ok := tableMap[p]; ok {
			return t
		}
	}

	// Try singular forms (remove trailing 's')
	if strings.HasSuffix(baseName, "s") {
		singular := baseName[:len(baseName)-1]
		if t, ok := tableMap[singular]; ok {
			return t
		}
		if t, ok := tableMap[strings.ToLower(singular)]; ok {
			return t
		}
	}

	return nil
}

// findPrimaryKeyColumn finds the primary key column of a table
func (i *Inferrer) findPrimaryKeyColumn(table *Table) *Column {
	// First, try to find a column marked as PK
	for _, col := range table.Columns {
		if col.PK {
			return col
		}
	}

	// Fallback: look for "id" column
	for _, col := range table.Columns {
		if strings.ToLower(col.Name) == "id" {
			return col
		}
	}

	// Fallback: look for column named table_id or tableId
	tableName := strings.ToLower(table.Name)
	// Remove trailing 's' for singular
	if strings.HasSuffix(tableName, "s") {
		tableName = tableName[:len(tableName)-1]
	}
	for _, col := range table.Columns {
		colLower := strings.ToLower(col.Name)
		if colLower == tableName+"_id" || colLower == tableName+"id" {
			return col
		}
	}

	return nil
}

// calculateFKConfidence calculates the confidence score for a foreign key relationship
func (i *Inferrer) calculateFKConfidence(sourceCol, targetCol *Column) float64 {
	confidence := 0.0

	// Column name match: 0.4
	confidence += 0.4

	// Value range match: 0.3
	if sourceCol.Stats != nil && targetCol.Stats != nil {
		if i.valueRangeMatches(sourceCol.Stats, targetCol.Stats) {
			confidence += 0.3
		}
	}

	// Cardinality match: 0.3
	if sourceCol.Stats != nil && targetCol.Stats != nil {
		if sourceCol.Stats.DistinctCount <= targetCol.Stats.DistinctCount {
			confidence += 0.3
		}
	}

	return confidence
}

// valueRangeMatches checks if source column values are within target column range
func (i *Inferrer) valueRangeMatches(source, target *ColumnStats) bool {
	// For numeric types with min/max
	if source.Min != nil && source.Max != nil && target.Min != nil && target.Max != nil {
		if *source.Min >= *target.Min && *source.Max <= *target.Max {
			return true
		}
	}
	return false
}

// inferBusinessInsights generates business-level insights
func (i *Inferrer) inferBusinessInsights(s *Schema) []BusinessInsight {
	var insights []BusinessInsight

	// Use inferred foreign keys to generate insights
	if s.Inferences == nil {
		return insights
	}

	// Build a map for quick table lookup
	tableMap := make(map[string]*Table)
	for _, t := range s.Tables {
		tableMap[t.Name] = t
	}

	// For each foreign key relationship, generate insights
	for _, fk := range s.Inferences.ForeignKeys {
		sourceTable := tableMap[fk.SourceTable]
		targetTable := tableMap[fk.TargetTable]

		if sourceTable == nil || targetTable == nil {
			continue
		}

		if sourceTable.Stats == nil || targetTable.Stats == nil {
			continue
		}

		sourceRowCount := sourceTable.Stats.RowCount
		targetRowCount := targetTable.Stats.RowCount

		if targetRowCount == 0 {
			continue
		}

		// Average per entity insight
		avgPerEntity := float64(sourceRowCount) / float64(targetRowCount)
		insights = append(insights, BusinessInsight{
			Type:        "avg_per_entity",
			Description: fmt.Sprintf("Average %s per %s", sourceTable.Name, targetTable.Name),
			Value:       avgPerEntity,
			Tables:      []string{sourceTable.Name, targetTable.Name},
		})

		// Entity coverage insight
		sourceCol, err := sourceTable.FindColumnByName(fk.SourceColumn)
		if err != nil {
			continue
		}
		if sourceCol.Stats != nil && sourceCol.Stats.DistinctCount > 0 {
			coverage := float64(sourceCol.Stats.DistinctCount) / float64(targetRowCount)
			if coverage > 1 {
				coverage = 1
			}
			insights = append(insights, BusinessInsight{
				Type:        "entity_coverage",
				Description: fmt.Sprintf("%s coverage rate (with %s)", targetTable.Name, sourceTable.Name),
				Value:       coverage,
				Tables:      []string{sourceTable.Name, targetTable.Name},
			})
		}
	}

	return insights
}
