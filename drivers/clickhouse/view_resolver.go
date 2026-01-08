package clickhouse

import (
	"regexp"

	"github.com/k1LoW/tbls/drivers"
	"github.com/k1LoW/tbls/schema"
	"github.com/sirupsen/logrus"
)

// TableMeta stores table/view metadata from system.tables
type TableMeta struct {
	Name         string
	Engine       string
	PartitionKey string
	AsSelect     string
}

// getTableMeta queries system.tables for table metadata
func (ch *ClickHouse) getTableMeta(dbName, tableName string) (*TableMeta, error) {
	query := `
		SELECT
			name,
			engine,
			partition_key,
			as_select
		FROM system.tables
		WHERE database = ? AND name = ?
	`
	row := ch.db.QueryRow(query, dbName, tableName)

	var meta TableMeta
	var partitionKey, asSelect *string
	if err := row.Scan(&meta.Name, &meta.Engine, &partitionKey, &asSelect); err != nil {
		return nil, err
	}
	if partitionKey != nil {
		meta.PartitionKey = *partitionKey
	}
	if asSelect != nil {
		meta.AsSelect = *asSelect
	}
	return &meta, nil
}

// isView checks if the engine is a View type
func isView(engine string) bool {
	return engine == "View" || engine == "MaterializedView"
}

// extractUnderlyingTable extracts the first table name from FROM clause
func extractUnderlyingTable(asSelect string) string {
	// Pattern: FROM [db.]table [AS alias]
	// Handles: FROM table, FROM db.table, FROM `table`, FROM "table"
	re := regexp.MustCompile(`(?i)\bFROM\s+` + "`?" + `"?(\w+)"?` + "`?" + `\.` + "`?" + `"?(\w+)"?` + "`?")
	matches := re.FindStringSubmatch(asSelect)
	if len(matches) >= 3 {
		return matches[2]
	}

	// Try simpler pattern without database prefix
	re2 := regexp.MustCompile(`(?i)\bFROM\s+` + "`?" + `"?(\w+)"?` + "`?")
	matches2 := re2.FindStringSubmatch(asSelect)
	if len(matches2) >= 2 {
		return matches2[1]
	}

	return ""
}

// findColumnMapping finds how sourceColumn is mapped in the SELECT clause
func findColumnMapping(asSelect, sourceColumn string) string {
	// Normalize whitespace
	normalized := regexp.MustCompile(`\s+`).ReplaceAllString(asSelect, " ")

	// Pattern 1: column AS alias (e.g., "__date AS dt" or "t.__date AS dt")
	re1 := regexp.MustCompile(`(?i)(\w+\.)?` + regexp.QuoteMeta(sourceColumn) + `\s+AS\s+(\w+)`)
	if matches := re1.FindStringSubmatch(normalized); len(matches) >= 3 {
		log.WithFields(logrus.Fields{
			"source_column": sourceColumn,
			"mapped_to":     matches[2],
			"pattern":       "column AS alias",
		}).Debug("found column mapping")
		return matches[2]
	}

	// Pattern 2: direct reference (e.g., "__date" or "t.__date" without AS)
	re2 := regexp.MustCompile(`(?i)(\w+\.)?` + regexp.QuoteMeta(sourceColumn) + `\b`)
	if re2.MatchString(normalized) {
		log.WithFields(logrus.Fields{
			"source_column": sourceColumn,
			"mapped_to":     sourceColumn,
			"pattern":       "direct reference",
		}).Debug("found column mapping")
		return sourceColumn
	}

	// Pattern 3: function wrapping with AS (e.g., "toDate(event_time) AS __date")
	// Check if sourceColumn appears as an alias after AS
	re3 := regexp.MustCompile(`(?i)\w+\([^)]+\)\s+AS\s+` + regexp.QuoteMeta(sourceColumn) + `\b`)
	if re3.MatchString(normalized) {
		log.WithFields(logrus.Fields{
			"source_column": sourceColumn,
			"mapped_to":     sourceColumn,
			"pattern":       "function AS alias",
		}).Debug("found column mapping")
		return sourceColumn
	}

	return ""
}

// resolveViewDateColumn resolves the date column for a View
func (ch *ClickHouse) resolveViewDateColumn(dbName string, meta *TableMeta, visited map[string]bool) string {
	if meta.AsSelect == "" {
		log.WithFields(logrus.Fields{
			"view": meta.Name,
		}).Debug("view has no as_select")
		return ""
	}

	// Extract underlying table
	underlyingTable := extractUnderlyingTable(meta.AsSelect)
	if underlyingTable == "" {
		log.WithFields(logrus.Fields{
			"view":      meta.Name,
			"as_select": truncateString(meta.AsSelect, 100),
		}).Warn("could not extract underlying table from view")
		return ""
	}

	log.WithFields(logrus.Fields{
		"view":             meta.Name,
		"underlying_table": underlyingTable,
	}).Debug("extracted underlying table")

	// Prevent infinite recursion
	if visited[underlyingTable] {
		log.WithFields(logrus.Fields{
			"view":             meta.Name,
			"underlying_table": underlyingTable,
		}).Warn("circular reference detected")
		return ""
	}
	visited[underlyingTable] = true

	// Get underlying table's date column (recursive)
	underlyingDateCol := ch.getDateColumnForTableInternal(dbName, underlyingTable, nil, visited)
	if underlyingDateCol == "" {
		log.WithFields(logrus.Fields{
			"view":             meta.Name,
			"underlying_table": underlyingTable,
		}).Debug("underlying table has no date column")
		return ""
	}

	// Find column mapping in SELECT clause
	mappedCol := findColumnMapping(meta.AsSelect, underlyingDateCol)
	if mappedCol == "" {
		log.WithFields(logrus.Fields{
			"view":                meta.Name,
			"underlying_date_col": underlyingDateCol,
			"suggestion":          "add stats.tables." + meta.Name + ".dateColumn in config",
		}).Warn("could not find column mapping in view, proceeding without date filter")
		return ""
	}

	log.WithFields(logrus.Fields{
		"view":                meta.Name,
		"underlying_date_col": underlyingDateCol,
		"view_date_col":       mappedCol,
	}).Debug("resolved view date column")

	return mappedCol
}

// truncateString truncates a string to maxLen with ellipsis
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// getDateColumnForTable returns the date column for filtering stats queries
// Priority: user config > auto-detect view > auto-detect table > empty
func (ch *ClickHouse) getDateColumnForTable(dbName, tableName string, cfg drivers.StatsConfig) string {
	return ch.getDateColumnForTableInternal(dbName, tableName, cfg.Tables, make(map[string]bool))
}

// findTableConfig searches for a table config by name in the list
func findTableConfig(tables []drivers.TableStatsConfig, tableName string) *drivers.TableStatsConfig {
	for i := range tables {
		if tables[i].Name == tableName {
			return &tables[i]
		}
	}
	return nil
}

// getDateColumnForTableInternal is the internal implementation with recursion tracking
func (ch *ClickHouse) getDateColumnForTableInternal(
	dbName, tableName string,
	tables []drivers.TableStatsConfig,
	visited map[string]bool,
) string {
	// Priority 1: Table-level config
	if tableConfig := findTableConfig(tables, tableName); tableConfig != nil && tableConfig.DateColumn != "" {
		log.WithFields(logrus.Fields{
			"table":       tableName,
			"date_column": tableConfig.DateColumn,
			"source":      "table config",
		}).Debug("using configured date column")
		return tableConfig.DateColumn
	}

	// Priority 2: Auto-detect
	meta, err := ch.getTableMeta(dbName, tableName)
	if err != nil {
		log.WithFields(logrus.Fields{
			"table": tableName,
			"error": err.Error(),
		}).Debug("could not get table metadata")
		return ""
	}

	// Priority 3: View - resolve from underlying table
	if isView(meta.Engine) {
		log.WithFields(logrus.Fields{
			"table":  tableName,
			"engine": meta.Engine,
		}).Debug("detected view, resolving date column")
		return ch.resolveViewDateColumn(dbName, meta, visited)
	}

	// Priority 4: Regular table - use partition key
	if meta.PartitionKey != "" {
		dateCol := extractDateColumn(meta.PartitionKey)
		if dateCol != "" {
			log.WithFields(logrus.Fields{
				"table":         tableName,
				"partition_key": meta.PartitionKey,
				"date_column":   dateCol,
				"source":        "partition key",
			}).Debug("detected date column from partition key")
			return dateCol
		}
	}

	log.WithFields(logrus.Fields{
		"table":      tableName,
		"engine":     meta.Engine,
		"suggestion": "add stats.tables." + tableName + ".dateColumn in config",
	}).Debug("could not detect date column")
	return ""
}

// DetectDateColumns implements the drivers.DateColumnDetector interface.
// It returns a map of table name to detected date column for each table in the schema.
func (ch *ClickHouse) DetectDateColumns(s *schema.Schema) (map[string]string, error) {
	result := make(map[string]string)

	for _, table := range s.Tables {
		// Use nil for tables config since we want pure auto-detection
		dateCol := ch.getDateColumnForTableInternal(s.Name, table.Name, nil, make(map[string]bool))
		if dateCol != "" {
			result[table.Name] = dateCol
		}
	}

	return result, nil
}
