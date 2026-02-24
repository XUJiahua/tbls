package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/k1LoW/tbls/drivers"
	"github.com/k1LoW/tbls/schema"
	"github.com/sirupsen/logrus"
)

// numericTypeRe matches PostgreSQL numeric types
var numericTypeRe = regexp.MustCompile(`(?i)^(integer|bigint|smallint|numeric|decimal|real|double precision|serial|bigserial|smallserial|int2|int4|int8|float4|float8)(\(.+\))?$`)

// dateTimeTypeRe matches PostgreSQL date/time types
var dateTimeTypeRe = regexp.MustCompile(`(?i)^(date|timestamp|timestamptz|timestamp with(out)? time zone|time|timetz|time with(out)? time zone)(\(.+\))?$`)

// stringTypeRe matches PostgreSQL string types
var stringTypeRe = regexp.MustCompile(`(?i)^(text|varchar|character varying|char|character|uuid|name|citext)(\(.+\))?$`)

// complexTypeRe matches PostgreSQL types that should be skipped for deep analysis
var complexTypeRe = regexp.MustCompile(`(?i)^(json|jsonb|xml|bytea|tsvector|tsquery)$|(\[\])$`)

// ErrCancelled is returned when the operation is cancelled
var ErrCancelled = fmt.Errorf("operation cancelled")

// quoteIdent quotes a PostgreSQL identifier
func quoteIdent(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

// splitSchemaTable splits a "schema.table" name into schema and table parts
func splitSchemaTable(name string) (string, string) {
	parts := strings.SplitN(name, ".", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "public", name
}

// quotedFullName returns a properly quoted "schema"."table" string
func quotedFullName(name string) string {
	schemaName, tableName := splitSchemaTable(name)
	return quoteIdent(schemaName) + "." + quoteIdent(tableName)
}

// CollectStats collects statistics for all tables in the schema
func (p *Postgres) CollectStats(s *schema.Schema, cfg drivers.StatsConfig) error {
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}

	log.WithFields(logrus.Fields{
		"database":    s.Name,
		"use_pgstats": cfg.UsePgStats,
		"include":     cfg.Include,
		"exclude":     cfg.Exclude,
	}).Debug("starting PostgreSQL stats collection")

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

		// Collect table-level stats
		tableStats, err := p.getTableStats(ctx, table.Name)
		if err != nil {
			log.WithFields(logrus.Fields{
				"table": table.Name,
				"error": err,
			}).Warn("failed to get table stats")
		} else {
			table.Stats = tableStats
			log.WithFields(logrus.Fields{
				"table":      table.Name,
				"row_count":  tableStats.RowCount,
				"data_bytes": tableStats.DataBytes,
			}).Debug("table stats")
		}

		// Determine sampling mode
		var useDateFilter bool
		var dateCol string
		var sampleSize int

		tableConfig := cfg.GetTableConfig(table.Name)
		if tableConfig != nil {
			switch tableConfig.Mode {
			case drivers.SamplingModeDateFilter:
				useDateFilter = true
				dateCol = tableConfig.DateColumn
				log.WithFields(logrus.Fields{
					"table":       table.Name,
					"mode":        "date_filter",
					"date_column": dateCol,
					"recent_days": cfg.RecentDays,
				}).Debug("using date filter sampling")
			case drivers.SamplingModeRowLimit:
				useDateFilter = false
				sampleSize = tableConfig.SampleSize
				log.WithFields(logrus.Fields{
					"table":       table.Name,
					"mode":        "row_limit",
					"sample_size": sampleSize,
				}).Debug("using row limit sampling")
			default:
				useDateFilter = false
				sampleSize = cfg.SampleSize
			}
		} else {
			useDateFilter = false
			sampleSize = cfg.SampleSize
		}

		// Collect column stats
		if cfg.UsePgStats {
			// Mode B: Use pg_stats system view
			err := p.collectColumnStatsFromPgStats(ctx, table, cfg.TopN, cfg.Checkpoint, cfg.Progress, &completedColumns, totalColumns)
			if err != nil {
				if cfg.Checkpoint != nil {
					_ = cfg.Checkpoint.Save()
				}
				return err
			}
		} else {
			// Mode A: Use direct queries
			for colIdx, col := range table.Columns {
				// Check for cancellation
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

				colStats, err := p.getColumnStats(ctx, table.Name, col.Name, col.Type, useDateFilter, dateCol, sampleSize, cfg.RecentDays, cfg.TopN)
				if err != nil {
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
	for _, pattern := range exclude {
		if matchPattern(pattern, tableName) {
			return false
		}
	}
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

// getTableStats queries pg_stat_user_tables and pg_table_size/pg_indexes_size for table-level stats
func (p *Postgres) getTableStats(ctx context.Context, tableName string) (*schema.TableStats, error) {
	schemaName, tblName := splitSchemaTable(tableName)

	query := `
		SELECT
			COALESCE(s.n_live_tup, 0) AS row_count,
			COALESCE(pg_table_size(c.oid), 0) AS data_bytes,
			COALESCE(pg_indexes_size(c.oid), 0) AS index_bytes
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		LEFT JOIN pg_stat_user_tables s ON s.relid = c.oid
		WHERE n.nspname = $1 AND c.relname = $2
	`

	log.WithField("query", strings.TrimSpace(query)).Debug("executing SQL")
	startTime := time.Now()
	row := p.db.QueryRowContext(ctx, query, schemaName, tblName)
	duration := time.Since(startTime).Milliseconds()

	var rowCount, dataBytes, indexBytes int64
	if err := row.Scan(&rowCount, &dataBytes, &indexBytes); err != nil {
		if err == sql.ErrNoRows {
			return &schema.TableStats{}, nil
		}
		return nil, fmt.Errorf("table stats query failed for %s: %w", tableName, err)
	}

	formattedQuery := fmt.Sprintf("SELECT ... FROM pg_class WHERE nspname = '%s' AND relname = '%s'", schemaName, tblName)
	return &schema.TableStats{
		RowCount:   rowCount,
		DataBytes:  dataBytes,
		IndexBytes: indexBytes,
		Queries:    []schema.QueryRecord{{SQL: formattedQuery, DurationMs: duration}},
	}, nil
}

// getColumnStats collects column stats using direct SQL queries (Mode A)
func (p *Postgres) getColumnStats(ctx context.Context, tableName, colName, colType string, useDateFilter bool, dateColumn string, sampleSize int, recentDays int, topN int) (*schema.ColumnStats, error) {
	stats := &schema.ColumnStats{}
	isNumeric := numericTypeRe.MatchString(colType)
	isDate := dateTimeTypeRe.MatchString(colType)
	isString := stringTypeRe.MatchString(colType)
	isComplex := complexTypeRe.MatchString(colType)

	quotedCol := quoteIdent(colName)
	quotedTable := quotedFullName(tableName)

	// Build WHERE clause for date filtering
	whereClause := ""
	if useDateFilter && dateColumn != "" {
		whereClause = fmt.Sprintf("WHERE %s >= CURRENT_DATE - INTERVAL '%d days'", quoteIdent(dateColumn), recentDays)
	}

	// Build FROM clause
	var fromClause string
	if useDateFilter {
		fromClause = fmt.Sprintf("FROM %s %s", quotedTable, whereClause)
	} else if sampleSize == -1 {
		fromClause = fmt.Sprintf("FROM %s", quotedTable)
	} else {
		fromClause = fmt.Sprintf("FROM (SELECT %s FROM %s LIMIT %d) _sample", quotedCol, quotedTable, sampleSize)
	}

	// Handle complex types: row count only
	if isComplex {
		log.WithFields(logrus.Fields{
			"table":  tableName,
			"column": colName,
			"type":   colType,
		}).Debug("skipping complex analysis for non-basic type")

		minimalFromClause := fromClause
		if !useDateFilter && sampleSize != -1 {
			minimalFromClause = fmt.Sprintf("FROM (SELECT 1 FROM %s LIMIT %d) _sample", quotedTable, sampleSize)
		}
		query := fmt.Sprintf("SELECT count(*) AS row_count %s", minimalFromClause)

		log.WithField("query", strings.TrimSpace(query)).Debug("executing SQL")
		startTime := time.Now()
		row := p.db.QueryRowContext(ctx, query)
		var rowCount int64
		if err := row.Scan(&rowCount); err != nil {
			return nil, fmt.Errorf("minimal stats query failed: %w", err)
		}
		duration := time.Since(startTime).Milliseconds()

		stats.RowCount = rowCount
		stats.SkippedComplexAnalysis = true
		stats.Queries = append(stats.Queries, schema.QueryRecord{SQL: strings.TrimSpace(query), DurationMs: duration})
		return stats, nil
	}

	// Build type-specific query
	var query string

	// Fallback query (basic stats, no type-specific functions)
	fallbackQuery := fmt.Sprintf(`
		SELECT
			count(*) AS row_count,
			count(*) FILTER (WHERE %s IS NULL) AS null_count,
			count(DISTINCT %s) AS distinct_count,
			0::float8 AS min_val,
			0::float8 AS max_val,
			0::float8 AS avg_val,
			''::text AS min_date,
			''::text AS max_date,
			0::bigint AS min_len,
			0::bigint AS max_len,
			0::float8 AS avg_len
		%s
	`, quotedCol, quotedCol, fromClause)

	switch {
	case isNumeric:
		query = fmt.Sprintf(`
			SELECT
				count(*) AS row_count,
				count(*) FILTER (WHERE %s IS NULL) AS null_count,
				count(DISTINCT %s) AS distinct_count,
				COALESCE(min(%s)::float8, 0) AS min_val,
				COALESCE(max(%s)::float8, 0) AS max_val,
				COALESCE(avg(%s)::float8, 0) AS avg_val,
				''::text AS min_date,
				''::text AS max_date,
				0::bigint AS min_len,
				0::bigint AS max_len,
				0::float8 AS avg_len
			%s
		`, quotedCol, quotedCol, quotedCol, quotedCol, quotedCol, fromClause)
	case isDate:
		query = fmt.Sprintf(`
			SELECT
				count(*) AS row_count,
				count(*) FILTER (WHERE %s IS NULL) AS null_count,
				count(DISTINCT %s) AS distinct_count,
				0::float8 AS min_val,
				0::float8 AS max_val,
				0::float8 AS avg_val,
				COALESCE(min(%s)::text, '') AS min_date,
				COALESCE(max(%s)::text, '') AS max_date,
				0::bigint AS min_len,
				0::bigint AS max_len,
				0::float8 AS avg_len
			%s
		`, quotedCol, quotedCol, quotedCol, quotedCol, fromClause)
	case isString:
		query = fmt.Sprintf(`
			SELECT
				count(*) AS row_count,
				count(*) FILTER (WHERE %s IS NULL) AS null_count,
				count(DISTINCT %s) AS distinct_count,
				0::float8 AS min_val,
				0::float8 AS max_val,
				0::float8 AS avg_val,
				''::text AS min_date,
				''::text AS max_date,
				COALESCE(min(length(%s))::bigint, 0) AS min_len,
				COALESCE(max(length(%s))::bigint, 0) AS max_len,
				COALESCE(avg(length(%s))::float8, 0) AS avg_len
			%s
		`, quotedCol, quotedCol, quotedCol, quotedCol, quotedCol, fromClause)
	default:
		query = fallbackQuery
	}

	log.WithField("query", strings.TrimSpace(query)).Debug("executing SQL")
	startTime := time.Now()
	row := p.db.QueryRowContext(ctx, query)
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

	originalQuery := query
	if err := row.Scan(&rowCount, &nullCount, &distinctCount, &minVal, &maxVal, &avgVal, &minDate, &maxDate, &minLen, &maxLen, &avgLen); err != nil {
		if query != fallbackQuery {
			originalDuration := time.Since(startTime).Milliseconds()
			log.WithFields(logrus.Fields{
				"table":  tableName,
				"column": colName,
				"type":   colType,
				"error":  err.Error(),
			}).Warn("type-specific stats query failed, falling back to basic stats")

			fallbackError = err.Error()
			usedFallback = true

			stats.Queries = append(stats.Queries, schema.QueryRecord{SQL: strings.TrimSpace(originalQuery), DurationMs: originalDuration})

			// Reset type flags for fallback
			isNumeric = false
			isDate = false
			isString = false

			log.WithField("query", strings.TrimSpace(fallbackQuery)).Debug("executing fallback SQL")
			startTime = time.Now()
			row = p.db.QueryRowContext(ctx, fallbackQuery)
			if err := row.Scan(&rowCount, &nullCount, &distinctCount, &minVal, &maxVal, &avgVal, &minDate, &maxDate, &minLen, &maxLen, &avgLen); err != nil {
				fallbackDuration := time.Since(startTime).Milliseconds()
				log.WithFields(logrus.Fields{
					"table":  tableName,
					"column": colName,
					"error":  err.Error(),
				}).Warn("fallback stats query also failed, returning empty stats")

				stats.Fallback = true
				stats.FallbackError = fmt.Sprintf("both type-specific and fallback queries failed: %s", err.Error())
				stats.Queries = append(stats.Queries, schema.QueryRecord{SQL: strings.TrimSpace(fallbackQuery), DurationMs: fallbackDuration})

				// Still try top values
				topValues, topValuesQueryRecord, topErr := p.getTopValues(ctx, tableName, colName, whereClause, topN, useDateFilter, sampleSize)
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

	if usedFallback {
		stats.Fallback = true
		stats.FallbackError = fallbackError
	}

	stats.Queries = append(stats.Queries, schema.QueryRecord{SQL: strings.TrimSpace(query), DurationMs: duration})

	// Get top N values
	topValues, topValuesQueryRecord, err := p.getTopValues(ctx, tableName, colName, whereClause, topN, useDateFilter, sampleSize)
	if err == nil {
		stats.TopValues = topValues
		if topValuesQueryRecord.SQL != "" {
			stats.Queries = append(stats.Queries, topValuesQueryRecord)
		}
	}

	return stats, nil
}

func (p *Postgres) getTopValues(ctx context.Context, tableName, colName, whereClause string, topN int, useDateFilter bool, sampleSize int) ([]schema.TopValue, schema.QueryRecord, error) {
	quotedCol := quoteIdent(colName)
	quotedTable := quotedFullName(tableName)

	var query string
	if useDateFilter {
		query = fmt.Sprintf(`
			SELECT
				%s::text AS value,
				count(*) AS cnt
			FROM %s
			%s
			GROUP BY %s
			ORDER BY cnt DESC
			LIMIT %d
		`, quotedCol, quotedTable, whereClause, quotedCol, topN)
	} else if sampleSize == -1 {
		query = fmt.Sprintf(`
			SELECT
				%s::text AS value,
				count(*) AS cnt
			FROM %s
			GROUP BY %s
			ORDER BY cnt DESC
			LIMIT %d
		`, quotedCol, quotedTable, quotedCol, topN)
	} else {
		query = fmt.Sprintf(`
			SELECT
				value::text,
				count(*) AS cnt
			FROM (
				SELECT %s AS value
				FROM %s
				LIMIT %d
			) _sample
			GROUP BY value
			ORDER BY cnt DESC
			LIMIT %d
		`, quotedCol, quotedTable, sampleSize, topN)
	}

	formattedQuery := strings.TrimSpace(query)
	log.WithField("query", formattedQuery).Debug("executing SQL")
	startTime := time.Now()
	rows, err := p.db.QueryContext(ctx, query)
	duration := time.Since(startTime).Milliseconds()
	if err != nil {
		return nil, schema.QueryRecord{}, err
	}
	defer rows.Close()

	var result []schema.TopValue
	for rows.Next() {
		var (
			value sql.NullString
			count int64
		)
		if err := rows.Scan(&value, &count); err != nil {
			continue
		}
		v := ""
		if value.Valid {
			v = value.String
		}
		result = append(result, schema.TopValue{
			Value: v,
			Count: count,
		})
	}

	return result, schema.QueryRecord{SQL: formattedQuery, DurationMs: duration}, nil
}

// collectColumnStatsFromPgStats uses the pg_stats system view (Mode B)
func (p *Postgres) collectColumnStatsFromPgStats(ctx context.Context, table *schema.Table, topN int, checkpoint drivers.CheckpointUpdater, progress drivers.ProgressReporter, completedColumns *int, totalColumns int) error {
	schemaName, tblName := splitSchemaTable(table.Name)

	// Get row count from table stats (already collected)
	var rowCount int64
	if table.Stats != nil {
		rowCount = table.Stats.RowCount
	}

	for colIdx, col := range table.Columns {
		// Check for cancellation
		if ctx.Err() != nil {
			return ErrCancelled
		}
		if progress != nil && progress.IsCancelled() {
			return ErrCancelled
		}

		// Skip if column is already completed (from checkpoint)
		if checkpoint != nil && checkpoint.IsColumnCompleted(table.Name, col.Name) {
			*completedColumns++
			continue
		}

		// Report progress
		if progress != nil {
			progress.ReportColumn(table.Name, col.Name, *completedColumns, totalColumns)
		}

		log.WithFields(logrus.Fields{
			"table":    table.Name,
			"column":   col.Name,
			"type":     col.Type,
			"mode":     "pg_stats",
			"progress": fmt.Sprintf("%d/%d (%.1f%%)", colIdx+1, len(table.Columns), float64(*completedColumns)/float64(totalColumns)*100),
		}).Debug("collecting column stats from pg_stats")

		colStats, err := p.getColumnStatsFromPgStats(ctx, schemaName, tblName, col.Name, col.Type, rowCount, topN)
		if err != nil {
			return fmt.Errorf("failed to collect pg_stats for %s.%s: %w", table.Name, col.Name, err)
		}
		col.Stats = colStats
		*completedColumns++

		// Update checkpoint
		if checkpoint != nil {
			checkpoint.UpdateColumn(table.Name, col.Name, colStats)
		}
	}

	return nil
}

// getColumnStatsFromPgStats reads stats from pg_stats system view
func (p *Postgres) getColumnStatsFromPgStats(ctx context.Context, schemaName, tableName, colName, colType string, rowCount int64, topN int) (*schema.ColumnStats, error) {
	stats := &schema.ColumnStats{}
	isString := stringTypeRe.MatchString(colType)

	query := `
		SELECT
			null_frac,
			n_distinct,
			avg_width,
			most_common_vals::text,
			most_common_freqs::text
		FROM pg_stats
		WHERE schemaname = $1 AND tablename = $2 AND attname = $3
	`

	log.WithField("query", strings.TrimSpace(query)).Debug("executing SQL")
	startTime := time.Now()
	row := p.db.QueryRowContext(ctx, query, schemaName, tableName, colName)
	duration := time.Since(startTime).Milliseconds()

	var (
		nullFrac       sql.NullFloat64
		nDistinct      sql.NullFloat64
		avgWidth       sql.NullInt64
		mostCommonVals sql.NullString
		mostCommonFreq sql.NullString
	)

	if err := row.Scan(&nullFrac, &nDistinct, &avgWidth, &mostCommonVals, &mostCommonFreq); err != nil {
		if err == sql.ErrNoRows {
			// Column not in pg_stats (might need ANALYZE run)
			stats.Fallback = true
			stats.FallbackError = "column not found in pg_stats; run ANALYZE on the table"
			return stats, nil
		}
		return nil, fmt.Errorf("pg_stats query failed for %s.%s: %w", tableName, colName, err)
	}

	formattedQuery := fmt.Sprintf("SELECT ... FROM pg_stats WHERE schemaname='%s' AND tablename='%s' AND attname='%s'", schemaName, tableName, colName)
	stats.Queries = append(stats.Queries, schema.QueryRecord{SQL: formattedQuery, DurationMs: duration})

	stats.RowCount = rowCount

	// null_frac -> NullPercent and NullCount
	if nullFrac.Valid {
		stats.NullPercent = nullFrac.Float64 * 100
		stats.NullCount = int64(nullFrac.Float64 * float64(rowCount))
	}

	// n_distinct -> DistinctCount
	// Negative values mean fraction of rows (e.g., -1 means all unique)
	if nDistinct.Valid {
		if nDistinct.Float64 < 0 {
			stats.DistinctCount = int64(-nDistinct.Float64 * float64(rowCount))
		} else {
			stats.DistinctCount = int64(nDistinct.Float64)
		}
	}

	// avg_width -> AvgLength (for string columns)
	if isString && avgWidth.Valid {
		avgLen := float64(avgWidth.Int64)
		stats.AvgLength = &avgLen
	}

	// Parse most_common_vals and most_common_freqs into TopValues
	if mostCommonVals.Valid && mostCommonFreq.Valid {
		topValues := parsePgStatsTopValues(mostCommonVals.String, mostCommonFreq.String, rowCount, topN)
		stats.TopValues = topValues
	}

	// Mark as fallback since pg_stats doesn't provide exact MIN/MAX/AVG
	stats.Fallback = true
	stats.FallbackError = "pg_stats mode: values are estimated from pg_stats system view (no exact MIN/MAX/AVG)"

	return stats, nil
}

// parsePgStatsTopValues parses PostgreSQL array literal format from pg_stats
// most_common_vals looks like: {val1,val2,val3}
// most_common_freqs looks like: {0.3,0.2,0.1}
func parsePgStatsTopValues(valsStr, freqsStr string, rowCount int64, topN int) []schema.TopValue {
	vals := parsePgArray(valsStr)
	freqs := parsePgFloatArray(freqsStr)

	var result []schema.TopValue
	limit := len(vals)
	if limit > len(freqs) {
		limit = len(freqs)
	}
	if limit > topN {
		limit = topN
	}

	for i := 0; i < limit; i++ {
		count := int64(freqs[i] * float64(rowCount))
		result = append(result, schema.TopValue{
			Value: vals[i],
			Count: count,
		})
	}

	return result
}

// parsePgArray parses a PostgreSQL text array representation like {val1,val2,"val with comma"}
func parsePgArray(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" || s == "{}" {
		return nil
	}
	// Remove outer braces
	s = strings.TrimPrefix(s, "{")
	s = strings.TrimSuffix(s, "}")

	var result []string
	var current strings.Builder
	inQuote := false
	escaped := false

	for _, ch := range s {
		if escaped {
			current.WriteRune(ch)
			escaped = false
			continue
		}
		if ch == '\\' {
			escaped = true
			continue
		}
		if ch == '"' {
			inQuote = !inQuote
			continue
		}
		if ch == ',' && !inQuote {
			result = append(result, current.String())
			current.Reset()
			continue
		}
		current.WriteRune(ch)
	}
	if current.Len() > 0 {
		result = append(result, current.String())
	}

	return result
}

// parsePgFloatArray parses a PostgreSQL float array like {0.3,0.2,0.1}
func parsePgFloatArray(s string) []float64 {
	s = strings.TrimSpace(s)
	if s == "" || s == "{}" {
		return nil
	}
	s = strings.TrimPrefix(s, "{")
	s = strings.TrimSuffix(s, "}")

	parts := strings.Split(s, ",")
	var result []float64
	for _, p := range parts {
		var f float64
		if _, err := fmt.Sscanf(strings.TrimSpace(p), "%f", &f); err == nil {
			result = append(result, f)
		}
	}
	return result
}
