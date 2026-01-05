package clickhouse

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/k1LoW/tbls/drivers"
	"github.com/k1LoW/tbls/schema"
)

// isDebug checks if debug mode is enabled via DEBUG or TBLS_DEBUG environment variable
func isDebug() bool {
	for _, key := range []string{"DEBUG", "TBLS_DEBUG"} {
		env := os.Getenv(key)
		if env != "" {
			debug, _ := strconv.ParseBool(env)
			if debug {
				return true
			}
		}
	}
	return false
}

// logDebug prints debug message if debug mode is enabled
func logDebug(format string, args ...interface{}) {
	if isDebug() {
		fmt.Printf("[DEBUG] "+format+"\n", args...)
	}
}

// logSQL prints SQL query if debug mode is enabled
func logSQL(query string) {
	if isDebug() {
		fmt.Printf("[SQL] %s\n", strings.TrimSpace(query))
	}
}

// numericTypeRe matches ClickHouse numeric types
var numericTypeRe = regexp.MustCompile(`(?i)^(U?Int\d+|Float\d+|Decimal.*|Nullable\((U?Int\d+|Float\d+|Decimal.*)\))`)

// dateTypeRe matches ClickHouse date/datetime types
var dateTypeRe = regexp.MustCompile(`(?i)^(Date|Date32|DateTime|DateTime64.*|Nullable\((Date|Date32|DateTime|DateTime64.*)\))`)

// stringTypeRe matches ClickHouse string types
var stringTypeRe = regexp.MustCompile(`(?i)^(String|FixedString.*|UUID|Nullable\((String|FixedString.*|UUID)\))`)

// backquote escapes ClickHouse identifiers
func backquote(identifier string) string {
	return "`" + strings.ReplaceAll(identifier, "`", "``") + "`"
}

// ErrCancelled is returned when the operation is cancelled
var ErrCancelled = fmt.Errorf("operation cancelled")

// CollectStats collects statistics for all tables in the schema
func (ch *ClickHouse) CollectStats(s *schema.Schema, cfg drivers.StatsConfig) error {
	logDebug("Starting stats collection for database: %s", s.Name)
	logDebug("Include filter: %v", cfg.Include)
	logDebug("Exclude filter: %v", cfg.Exclude)

	// Get table stats from system.tables
	logDebug("Fetching table stats from system.tables...")
	tableStats, err := ch.getTableStats(s.Name)
	if err != nil {
		return err
	}
	logDebug("Found %d tables in system.tables", len(tableStats))

	// Calculate total columns for progress reporting
	totalColumns := 0
	totalTables := 0
	for _, table := range s.Tables {
		if shouldCollectStats(table.Name, cfg.Include, cfg.Exclude) {
			totalTables++
			totalColumns += len(table.Columns)
		}
	}
	logDebug("Will collect stats for %d tables, %d columns total", totalTables, totalColumns)

	completedColumns := 0
	completedTables := 0

	for _, table := range s.Tables {
		// Check if this table should have stats collected
		if !shouldCollectStats(table.Name, cfg.Include, cfg.Exclude) {
			logDebug("Skipping table %s (not in include list or in exclude list)", table.Name)
			continue
		}

		// Skip if table is already completed (from checkpoint)
		if cfg.Checkpoint != nil && cfg.Checkpoint.IsTableCompleted(table.Name) {
			logDebug("Skipping table %s (already completed from checkpoint)", table.Name)
			completedColumns += len(table.Columns)
			completedTables++
			continue
		}

		completedTables++
		logDebug("Processing table [%d/%d]: %s (%d columns)", completedTables, totalTables, table.Name, len(table.Columns))

		// Set table-level stats
		if stats, ok := tableStats[table.Name]; ok {
			table.Stats = stats
			logDebug("  Table %s: %d rows, %d bytes", table.Name, stats.RowCount, stats.DataBytes)
		}

		// Determine sampling strategy
		isLargeTable := table.Stats != nil && table.Stats.RowCount > cfg.LargeTableThreshold
		partitionKey := ch.getPartitionKey(s.Name, table.Name)
		if isLargeTable {
			logDebug("  Large table detected, will use sampling (partition key: %s)", partitionKey)
		}

		// Collect column stats
		for colIdx, col := range table.Columns {
			// Check for cancellation
			if cfg.Progress != nil && cfg.Progress.IsCancelled() {
				if cfg.Checkpoint != nil {
					_ = cfg.Checkpoint.Save()
				}
				return ErrCancelled
			}

			// Skip if column is already completed (from checkpoint)
			if cfg.Checkpoint != nil && cfg.Checkpoint.IsColumnCompleted(table.Name, col.Name) {
				completedColumns++
				continue
			}

			// Report progress
			if cfg.Progress != nil {
				cfg.Progress.ReportColumn(table.Name, col.Name, completedColumns, totalColumns)
			}

			logDebug("  Column [%d/%d] %s.%s (%s) - progress: %d/%d (%.1f%%)",
				colIdx+1, len(table.Columns), table.Name, col.Name, col.Type,
				completedColumns, totalColumns, float64(completedColumns)/float64(totalColumns)*100)

			colStats, err := ch.getColumnStats(s.Name, table.Name, col.Name, col.Type, isLargeTable, partitionKey, cfg)
			if err != nil {
				// Save checkpoint and return error
				if cfg.Checkpoint != nil {
					_ = cfg.Checkpoint.Save()
				}
				return fmt.Errorf("failed to collect stats for %s.%s: %w", table.Name, col.Name, err)
			}
			col.Stats = colStats
			completedColumns++

			// Update checkpoint
			if cfg.Checkpoint != nil {
				cfg.Checkpoint.UpdateColumn(table.Name, col.Name, colStats)
			}
		}

		logDebug("Completed table: %s", table.Name)

		// Mark table as completed
		if cfg.Checkpoint != nil {
			cfg.Checkpoint.MarkTableCompleted(table.Name)
		}
	}

	logDebug("Stats collection completed: %d tables, %d columns", completedTables, completedColumns)
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

	query := `
		SELECT
			name,
			coalesce(total_rows, 0) as total_rows,
			coalesce(total_bytes, 0) as total_bytes,
			0 as index_bytes
		FROM system.tables
		WHERE database = ?
	`
	logSQL(fmt.Sprintf("%s -- args: [%s]", query, dbName))
	rows, err := ch.db.Query(query, dbName)
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
	query := `
		SELECT partition_key
		FROM system.tables
		WHERE database = ? AND name = ?
	`
	logSQL(fmt.Sprintf("%s -- args: [%s, %s]", query, dbName, tableName))
	row := ch.db.QueryRow(query, dbName, tableName)
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
	isDate := dateTypeRe.MatchString(colType)
	isString := stringTypeRe.MatchString(colType)

	// Build WHERE clause for large table sampling
	whereClause := ""
	if isLargeTable {
		dateCol := extractDateColumn(partitionKey)
		if dateCol != "" {
			whereClause = fmt.Sprintf("WHERE %s >= today() - %d", backquote(dateCol), cfg.RecentDays)
		}
	}

	// Build query based on column type
	var query string
	quotedCol := backquote(colName)
	quotedDB := backquote(dbName)
	quotedTable := backquote(tableName)

	switch {
	case isNumeric:
		query = fmt.Sprintf(`
			SELECT
				count() as row_count,
				countIf(%s IS NULL) as null_count,
				countDistinct(%s) as distinct_count,
				min(%s) as min_val,
				max(%s) as max_val,
				avg(%s) as avg_val,
				'' as min_date,
				'' as max_date,
				0 as min_len,
				0 as max_len,
				0 as avg_len
			FROM (
				SELECT %s
				FROM %s.%s
				%s
				LIMIT %d
			)
		`, quotedCol, quotedCol, quotedCol, quotedCol, quotedCol, quotedCol, quotedDB, quotedTable, whereClause, cfg.SampleSize)
	case isDate:
		query = fmt.Sprintf(`
			SELECT
				count() as row_count,
				countIf(%s IS NULL) as null_count,
				countDistinct(%s) as distinct_count,
				0 as min_val,
				0 as max_val,
				0 as avg_val,
				toString(min(%s)) as min_date,
				toString(max(%s)) as max_date,
				0 as min_len,
				0 as max_len,
				0 as avg_len
			FROM (
				SELECT %s
				FROM %s.%s
				%s
				LIMIT %d
			)
		`, quotedCol, quotedCol, quotedCol, quotedCol, quotedCol, quotedDB, quotedTable, whereClause, cfg.SampleSize)
	case isString:
		query = fmt.Sprintf(`
			SELECT
				count() as row_count,
				countIf(%s IS NULL) as null_count,
				countDistinct(%s) as distinct_count,
				0 as min_val,
				0 as max_val,
				0 as avg_val,
				'' as min_date,
				'' as max_date,
				min(length(%s)) as min_len,
				max(length(%s)) as max_len,
				avg(length(%s)) as avg_len
			FROM (
				SELECT %s
				FROM %s.%s
				%s
				LIMIT %d
			)
		`, quotedCol, quotedCol, quotedCol, quotedCol, quotedCol, quotedCol, quotedDB, quotedTable, whereClause, cfg.SampleSize)
	default:
		query = fmt.Sprintf(`
			SELECT
				count() as row_count,
				countIf(%s IS NULL) as null_count,
				countDistinct(%s) as distinct_count,
				0 as min_val,
				0 as max_val,
				0 as avg_val,
				'' as min_date,
				'' as max_date,
				0 as min_len,
				0 as max_len,
				0 as avg_len
			FROM (
				SELECT %s
				FROM %s.%s
				%s
				LIMIT %d
			)
		`, quotedCol, quotedCol, quotedCol, quotedDB, quotedTable, whereClause, cfg.SampleSize)
	}

	logSQL(query)
	row := ch.db.QueryRow(query)
	var (
		rowCount      int64
		nullCount     int64
		distinctCount int64
		minVal        float64
		maxVal        float64
		avgVal        float64
		minDate       string
		maxDate       string
		minLen        int64
		maxLen        int64
		avgLen        float64
	)
	if err := row.Scan(&rowCount, &nullCount, &distinctCount, &minVal, &maxVal, &avgVal, &minDate, &maxDate, &minLen, &maxLen, &avgLen); err != nil {
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

	if isDate && minDate != "" {
		stats.MinDate = &minDate
		stats.MaxDate = &maxDate
	}

	if isString {
		stats.MinLength = &minLen
		stats.MaxLength = &maxLen
		stats.AvgLength = &avgLen
	}

	// Get top N values
	topValues, err := ch.getTopValues(dbName, tableName, colName, whereClause, cfg.TopN)
	if err == nil {
		stats.TopValues = topValues
	}

	return stats, nil
}

func (ch *ClickHouse) getTopValues(dbName, tableName, colName, whereClause string, topN int) ([]schema.TopValue, error) {
	quotedCol := backquote(colName)
	quotedDB := backquote(dbName)
	quotedTable := backquote(tableName)

	query := fmt.Sprintf(`
		SELECT
			toString(%s) as value,
			count() as cnt
		FROM %s.%s
		%s
		GROUP BY %s
		ORDER BY cnt DESC
		LIMIT %d
	`, quotedCol, quotedDB, quotedTable, whereClause, quotedCol, topN)

	logSQL(query)
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
