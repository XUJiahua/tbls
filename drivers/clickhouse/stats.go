package clickhouse

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/k1LoW/tbls/drivers"
	"github.com/k1LoW/tbls/schema"
	"github.com/sirupsen/logrus"
)

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
	// Get context or use background context for backward compatibility
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}

	log.WithFields(logrus.Fields{
		"database": s.Name,
		"include":  cfg.Include,
		"exclude":  cfg.Exclude,
	}).Debug("starting stats collection")

	// Get table stats from system.tables
	tableStats, err := ch.getTableStats(ctx, s.Name)
	if err != nil {
		return err
	}
	log.WithField("count", len(tableStats)).Debug("fetched table stats from system.tables")

	// Calculate total columns for progress reporting
	totalColumns := 0
	totalTables := 0
	for _, table := range s.Tables {
		if shouldCollectStats(table.Name, cfg.Include, cfg.Exclude) {
			totalTables++
			totalColumns += len(table.Columns)
		}
	}
	log.WithFields(logrus.Fields{
		"tables":  totalTables,
		"columns": totalColumns,
	}).Debug("will collect stats")

	completedColumns := 0
	completedTables := 0

	for _, table := range s.Tables {
		// Check if this table should have stats collected
		if !shouldCollectStats(table.Name, cfg.Include, cfg.Exclude) {
			log.WithField("table", table.Name).Debug("skipping table (not in include list or in exclude list)")
			continue
		}

		// Skip if table is already completed (from checkpoint)
		if cfg.Checkpoint != nil && cfg.Checkpoint.IsTableCompleted(table.Name) {
			log.WithField("table", table.Name).Debug("skipping table (already completed from checkpoint)")
			completedColumns += len(table.Columns)
			completedTables++
			continue
		}

		completedTables++
		log.WithFields(logrus.Fields{
			"table":    table.Name,
			"progress": fmt.Sprintf("%d/%d", completedTables, totalTables),
			"columns":  len(table.Columns),
		}).Debug("processing table")

		// Set table-level stats
		if stats, ok := tableStats[table.Name]; ok {
			table.Stats = stats
			log.WithFields(logrus.Fields{
				"table":      table.Name,
				"row_count":  stats.RowCount,
				"data_bytes": stats.DataBytes,
			}).Debug("table stats")
		}

		// Determine sampling mode for this table based on explicit Mode field
		var useDateFilter bool
		var dateCol string
		var sampleSize int

		tableConfig := cfg.GetTableConfig(table.Name)
		if tableConfig != nil {
			switch tableConfig.Mode {
			case drivers.SamplingModeDateFilter:
				// Mode: date_filter - use date column for filtering
				useDateFilter = true
				dateCol = tableConfig.DateColumn
				log.WithFields(logrus.Fields{
					"table":       table.Name,
					"mode":        "date_filter",
					"date_column": dateCol,
					"recent_days": cfg.RecentDays,
				}).Debug("using date filter sampling")
			case drivers.SamplingModeRowLimit:
				// Mode: row_limit - use sample size (-1 means no limit)
				useDateFilter = false
				sampleSize = tableConfig.SampleSize
				log.WithFields(logrus.Fields{
					"table":       table.Name,
					"mode":        "row_limit",
					"sample_size": sampleSize,
				}).Debug("using row limit sampling")
			default:
				// No mode specified, fall back to global sampleSize
				useDateFilter = false
				sampleSize = cfg.SampleSize
				log.WithFields(logrus.Fields{
					"table":       table.Name,
					"mode":        "row_limit (default)",
					"sample_size": sampleSize,
				}).Debug("using global sample size")
			}
		} else {
			// No table config, use global sampleSize
			useDateFilter = false
			sampleSize = cfg.SampleSize
			log.WithFields(logrus.Fields{
				"table":       table.Name,
				"mode":        "row_limit (global)",
				"sample_size": sampleSize,
			}).Debug("using global sample size")
		}

		// Collect column stats
		for colIdx, col := range table.Columns {
			// Check for cancellation - context first (faster response), then progress reporter
			if ctx.Err() != nil {
				if cfg.Checkpoint != nil {
					_ = cfg.Checkpoint.Save()
				}
				return ErrCancelled
			}
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

			log.WithFields(logrus.Fields{
				"table":    table.Name,
				"column":   col.Name,
				"type":     col.Type,
				"progress": fmt.Sprintf("%d/%d (%.1f%%)", colIdx+1, len(table.Columns), float64(completedColumns)/float64(totalColumns)*100),
			}).Debug("collecting column stats")

			colStats, err := ch.getColumnStats(ctx, s.Name, table.Name, col.Name, col.Type, useDateFilter, dateCol, sampleSize, cfg.RecentDays, cfg.TopN)
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

		log.WithField("table", table.Name).Debug("completed table")

		// Mark table as completed
		if cfg.Checkpoint != nil {
			cfg.Checkpoint.MarkTableCompleted(table.Name)
		}
	}

	log.WithFields(logrus.Fields{
		"tables":  completedTables,
		"columns": completedColumns,
	}).Debug("stats collection completed")
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

func (ch *ClickHouse) getTableStats(ctx context.Context, dbName string) (map[string]*schema.TableStats, error) {
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
	log.WithField("query", strings.TrimSpace(query)).Debug("executing SQL")
	startTime := time.Now()
	rows, err := ch.db.QueryContext(ctx, query, dbName)
	duration := time.Since(startTime).Milliseconds()
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Format query with actual parameter for recording
	formattedQuery := strings.TrimSpace(strings.Replace(query, "?", "'"+dbName+"'", 1))

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
			Queries:    []schema.QueryRecord{{SQL: formattedQuery, DurationMs: duration}},
		}
	}

	return result, nil
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

func (ch *ClickHouse) getColumnStats(ctx context.Context, dbName, tableName, colName, colType string, useDateFilter bool, dateColumn string, sampleSize int, recentDays int, topN int) (*schema.ColumnStats, error) {
	stats := &schema.ColumnStats{}
	isNumeric := numericTypeRe.MatchString(colType)
	isDate := dateTypeRe.MatchString(colType)
	isString := stringTypeRe.MatchString(colType)

	// Build WHERE clause for date filtering
	whereClause := ""
	if useDateFilter && dateColumn != "" {
		whereClause = fmt.Sprintf("WHERE %s >= today() - %d", backquote(dateColumn), recentDays)
		log.WithFields(logrus.Fields{
			"table":        tableName,
			"column":       colName,
			"date_column":  dateColumn,
			"where_clause": whereClause,
		}).Debug("applying date filter")
	}

	// Build query based on column type
	var query string
	quotedCol := backquote(colName)
	quotedDB := backquote(dbName)
	quotedTable := backquote(tableName)

	// Build the FROM clause:
	// - If using date filter (recentDays), query all data within date range
	// - If sampleSize is -1, query all data (no limit)
	// - Otherwise, apply sampleSize limit for sampling
	var fromClause string
	if useDateFilter {
		// Date-filtered query - use all data within the date range
		fromClause = fmt.Sprintf("FROM %s.%s %s", quotedDB, quotedTable, whereClause)
	} else if sampleSize == -1 {
		// No limit - query all data
		fromClause = fmt.Sprintf("FROM %s.%s", quotedDB, quotedTable)
	} else {
		// Row limit sampling - apply sampleSize limit
		fromClause = fmt.Sprintf("FROM (SELECT %s FROM %s.%s LIMIT %d)", quotedCol, quotedDB, quotedTable, sampleSize)
	}

	// Build fallback query (basic stats only, no type-specific functions)
	fallbackQuery := fmt.Sprintf(`
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
		%s
	`, quotedCol, quotedCol, fromClause)

	switch {
	case isNumeric:
		query = fmt.Sprintf(`
			SELECT
				count() as row_count,
				countIf(%s IS NULL) as null_count,
				countDistinct(%s) as distinct_count,
				ifNull(min(%s), 0) as min_val,
				ifNull(max(%s), 0) as max_val,
				ifNull(avg(%s), 0) as avg_val,
				'' as min_date,
				'' as max_date,
				0 as min_len,
				0 as max_len,
				0 as avg_len
			%s
		`, quotedCol, quotedCol, quotedCol, quotedCol, quotedCol, fromClause)
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
			%s
		`, quotedCol, quotedCol, quotedCol, quotedCol, fromClause)
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
				ifNull(min(length(%s)), 0) as min_len,
				ifNull(max(length(%s)), 0) as max_len,
				ifNull(avg(length(%s)), 0) as avg_len
			%s
		`, quotedCol, quotedCol, quotedCol, quotedCol, quotedCol, fromClause)
	default:
		query = fallbackQuery
	}

	log.WithField("query", strings.TrimSpace(query)).Debug("executing SQL")
	startTime := time.Now()
	row := ch.db.QueryRowContext(ctx, query)
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

	usedFallback := false
	var fallbackError string

	if err := row.Scan(&rowCount, &nullCount, &distinctCount, &minVal, &maxVal, &avgVal, &minDate, &maxDate, &minLen, &maxLen, &avgLen); err != nil {
		// If the type-specific query failed, try the fallback query
		if query != fallbackQuery {
			log.WithFields(logrus.Fields{
				"table":  tableName,
				"column": colName,
				"type":   colType,
				"error":  err.Error(),
			}).Warn("type-specific stats query failed, falling back to basic stats")

			fallbackError = err.Error()
			usedFallback = true

			// Reset type flags since we're falling back
			isNumeric = false
			isDate = false
			isString = false

			log.WithField("query", strings.TrimSpace(fallbackQuery)).Debug("executing fallback SQL")
			startTime = time.Now()
			row = ch.db.QueryRowContext(ctx, fallbackQuery)
			if err := row.Scan(&rowCount, &nullCount, &distinctCount, &minVal, &maxVal, &avgVal, &minDate, &maxDate, &minLen, &maxLen, &avgLen); err != nil {
				// Even fallback failed (e.g., view with broken type casting)
				// Return empty stats with error recorded instead of failing entirely
				log.WithFields(logrus.Fields{
					"table":  tableName,
					"column": colName,
					"error":  err.Error(),
				}).Warn("fallback stats query also failed, returning empty stats")

				stats.Fallback = true
				stats.FallbackError = fmt.Sprintf("both type-specific and fallback queries failed: %s", err.Error())

				// Still try to get top values - they might work
				topValues, topValuesQueryRecord, topErr := ch.getTopValues(ctx, dbName, tableName, colName, whereClause, topN, useDateFilter, sampleSize)
				if topErr == nil {
					stats.TopValues = topValues
					if topValuesQueryRecord.SQL != "" {
						stats.Queries = append(stats.Queries, topValuesQueryRecord)
					}
				}

				return stats, nil
			}
			query = fallbackQuery
		} else {
			return nil, err
		}
	}
	duration := time.Since(startTime).Milliseconds()

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

	// Record fallback status
	if usedFallback {
		stats.Fallback = true
		stats.FallbackError = fallbackError
	}

	// Record the stats query
	stats.Queries = append(stats.Queries, schema.QueryRecord{SQL: strings.TrimSpace(query), DurationMs: duration})

	// Get top N values
	topValues, topValuesQueryRecord, err := ch.getTopValues(ctx, dbName, tableName, colName, whereClause, topN, useDateFilter, sampleSize)
	if err == nil {
		stats.TopValues = topValues
		if topValuesQueryRecord.SQL != "" {
			stats.Queries = append(stats.Queries, topValuesQueryRecord)
		}
	}

	return stats, nil
}

func (ch *ClickHouse) getTopValues(ctx context.Context, dbName, tableName, colName, whereClause string, topN int, useDateFilter bool, sampleSize int) ([]schema.TopValue, schema.QueryRecord, error) {
	quotedCol := backquote(colName)
	quotedDB := backquote(dbName)
	quotedTable := backquote(tableName)

	// Build top values query based on sampling mode:
	// - If using date filter, query the filtered data directly
	// - If sampleSize is -1, query all data (no limit)
	// - Otherwise, apply sampleSize to keep top_values consistent with other stats
	var query string
	if useDateFilter {
		// Date-filtered query - use all data within the date range
		query = fmt.Sprintf(`
			SELECT
				toString(%s) as value,
				count() as cnt
			FROM %s.%s
			%s
			GROUP BY %s
			ORDER BY cnt DESC
			LIMIT %d
		`, quotedCol, quotedDB, quotedTable, whereClause, quotedCol, topN)
	} else if sampleSize == -1 {
		// No limit - query all data
		query = fmt.Sprintf(`
			SELECT
				toString(%s) as value,
				count() as cnt
			FROM %s.%s
			GROUP BY %s
			ORDER BY cnt DESC
			LIMIT %d
		`, quotedCol, quotedDB, quotedTable, quotedCol, topN)
	} else {
		// Row limit sampling - apply sampleSize to be consistent with other stats
		query = fmt.Sprintf(`
			SELECT
				toString(value) as value,
				count() as cnt
			FROM (
				SELECT %s as value
				FROM %s.%s
				LIMIT %d
			)
			GROUP BY value
			ORDER BY cnt DESC
			LIMIT %d
		`, quotedCol, quotedDB, quotedTable, sampleSize, topN)
	}

	formattedQuery := strings.TrimSpace(query)
	log.WithField("query", formattedQuery).Debug("executing SQL")
	startTime := time.Now()
	rows, err := ch.db.QueryContext(ctx, query)
	duration := time.Since(startTime).Milliseconds()
	if err != nil {
		return nil, schema.QueryRecord{}, err
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

	return result, schema.QueryRecord{SQL: formattedQuery, DurationMs: duration}, nil
}
