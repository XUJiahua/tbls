package clickhouse

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/k1LoW/tbls/drivers"
	"github.com/k1LoW/tbls/schema"
)

// numericTypeRe matches ClickHouse numeric types
var numericTypeRe = regexp.MustCompile(`(?i)^(U?Int\d+|Float\d+|Decimal.*|Nullable\((U?Int\d+|Float\d+|Decimal.*)\))`)

// CollectStats collects statistics for all tables in the schema
func (ch *ClickHouse) CollectStats(s *schema.Schema, cfg drivers.StatsConfig) error {
	// Get table stats from system.tables
	tableStats, err := ch.getTableStats(s.Name)
	if err != nil {
		return err
	}

	for _, table := range s.Tables {
		// Check if this table should have stats collected
		if !shouldCollectStats(table.Name, cfg.Include, cfg.Exclude) {
			continue
		}

		// Set table-level stats
		if stats, ok := tableStats[table.Name]; ok {
			table.Stats = stats
		}

		// Determine sampling strategy
		isLargeTable := table.Stats != nil && table.Stats.RowCount > cfg.LargeTableThreshold
		partitionKey := ch.getPartitionKey(s.Name, table.Name)

		// Collect column stats
		for _, col := range table.Columns {
			colStats, err := ch.getColumnStats(s.Name, table.Name, col.Name, col.Type, isLargeTable, partitionKey, cfg)
			if err != nil {
				// Log error but continue with other columns
				continue
			}
			col.Stats = colStats
		}
	}

	return nil
}

func shouldCollectStats(tableName string, include, exclude []string) bool {
	// Check exclude first
	for _, pattern := range exclude {
		if matchPattern(pattern, tableName) {
			return false
		}
	}
	// If include is specified, table must match
	if len(include) > 0 {
		for _, pattern := range include {
			if matchPattern(pattern, tableName) {
				return true
			}
		}
		return false
	}
	return true
}

func matchPattern(pattern, name string) bool {
	// Simple wildcard matching
	if strings.Contains(pattern, "*") {
		pattern = strings.ReplaceAll(pattern, "*", ".*")
		re, err := regexp.Compile("^" + pattern + "$")
		if err != nil {
			return false
		}
		return re.MatchString(name)
	}
	return pattern == name
}

func (ch *ClickHouse) getTableStats(dbName string) (map[string]*schema.TableStats, error) {
	result := make(map[string]*schema.TableStats)

	rows, err := ch.db.Query(`
		SELECT
			name,
			total_rows,
			total_bytes,
			0 as index_bytes
		FROM system.tables
		WHERE database = ?
	`, dbName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			name       string
			rowCount   uint64
			dataBytes  uint64
			indexBytes uint64
		)
		if err := rows.Scan(&name, &rowCount, &dataBytes, &indexBytes); err != nil {
			continue
		}
		result[name] = &schema.TableStats{
			RowCount:   int64(rowCount),
			DataBytes:  int64(dataBytes),
			IndexBytes: int64(indexBytes),
		}
	}

	return result, nil
}

func (ch *ClickHouse) getPartitionKey(dbName, tableName string) string {
	var partitionKey string
	row := ch.db.QueryRow(`
		SELECT partition_key
		FROM system.tables
		WHERE database = ? AND name = ?
	`, dbName, tableName)
	_ = row.Scan(&partitionKey)
	return partitionKey
}

// extractDateColumn attempts to extract date column from partition key expression
func extractDateColumn(partitionKey string) string {
	// Match patterns like toYYYYMM(date), toDate(timestamp), etc.
	re := regexp.MustCompile(`to\w+\((\w+)\)`)
	matches := re.FindStringSubmatch(partitionKey)
	if len(matches) > 1 {
		return matches[1]
	}
	// If simple column name
	if !strings.Contains(partitionKey, "(") && partitionKey != "" {
		return strings.TrimSpace(partitionKey)
	}
	return ""
}

func (ch *ClickHouse) getColumnStats(dbName, tableName, colName, colType string, isLargeTable bool, partitionKey string, cfg drivers.StatsConfig) (*schema.ColumnStats, error) {
	stats := &schema.ColumnStats{}
	isNumeric := numericTypeRe.MatchString(colType)

	// Build WHERE clause for large table sampling
	whereClause := ""
	if isLargeTable {
		dateCol := extractDateColumn(partitionKey)
		if dateCol != "" {
			whereClause = fmt.Sprintf("WHERE %s >= today() - %d", dateCol, cfg.RecentDays)
		}
	}

	// Build query based on column type
	var query string
	if isNumeric {
		query = fmt.Sprintf(`
			SELECT
				count() as row_count,
				countIf(%s IS NULL) as null_count,
				countDistinct(%s) as distinct_count,
				min(%s) as min_val,
				max(%s) as max_val,
				avg(%s) as avg_val
			FROM %s.%s
			%s
			LIMIT %d
		`, colName, colName, colName, colName, colName, dbName, tableName, whereClause, cfg.SampleSize)
	} else {
		query = fmt.Sprintf(`
			SELECT
				count() as row_count,
				countIf(%s IS NULL) as null_count,
				countDistinct(%s) as distinct_count,
				0 as min_val,
				0 as max_val,
				0 as avg_val
			FROM %s.%s
			%s
			LIMIT %d
		`, colName, colName, dbName, tableName, whereClause, cfg.SampleSize)
	}

	row := ch.db.QueryRow(query)
	var (
		rowCount      int64
		nullCount     int64
		distinctCount int64
		minVal        float64
		maxVal        float64
		avgVal        float64
	)
	if err := row.Scan(&rowCount, &nullCount, &distinctCount, &minVal, &maxVal, &avgVal); err != nil {
		return nil, err
	}

	stats.RowCount = rowCount
	stats.NullCount = nullCount
	stats.DistinctCount = distinctCount
	if rowCount > 0 {
		stats.NullPercent = float64(nullCount) / float64(rowCount) * 100
	}

	if isNumeric {
		stats.Min = &minVal
		stats.Max = &maxVal
		stats.Avg = &avgVal
	}

	// Get top N values
	topValues, err := ch.getTopValues(dbName, tableName, colName, whereClause, cfg.TopN)
	if err == nil {
		stats.TopValues = topValues
	}

	return stats, nil
}

func (ch *ClickHouse) getTopValues(dbName, tableName, colName, whereClause string, topN int) ([]schema.TopValue, error) {
	query := fmt.Sprintf(`
		SELECT
			toString(%s) as value,
			count() as cnt
		FROM %s.%s
		%s
		GROUP BY %s
		ORDER BY cnt DESC
		LIMIT %d
	`, colName, dbName, tableName, whereClause, colName, topN)

	rows, err := ch.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []schema.TopValue
	for rows.Next() {
		var (
			value string
			count int64
		)
		if err := rows.Scan(&value, &count); err != nil {
			continue
		}
		result = append(result, schema.TopValue{
			Value: value,
			Count: count,
		})
	}

	return result, nil
}
