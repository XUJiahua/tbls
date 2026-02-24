package postgres

import (
	"fmt"
	"strings"

	"github.com/k1LoW/tbls/schema"
	"github.com/sirupsen/logrus"
)

// commonDateColumnNames are column names commonly used for date/time fields, in priority order
var commonDateColumnNames = []string{
	"created_at",
	"updated_at",
	"date",
	"timestamp",
	"created_date",
	"event_date",
	"event_time",
}

// dateColumnTypes are PostgreSQL types considered date/time types
var dateColumnTypes = map[string]bool{
	"date":                        true,
	"timestamp":                   true,
	"timestamptz":                 true,
	"timestamp with time zone":    true,
	"timestamp without time zone": true,
	"time":                        true,
	"timetz":                      true,
	"time with time zone":         true,
	"time without time zone":      true,
}

// isDateType checks if a PostgreSQL column type is a date/time type
func isDateType(colType string) bool {
	lower := strings.ToLower(colType)
	return dateColumnTypes[lower]
}

// DetectDateColumns detects the most appropriate date column for each table.
// Detection strategy (priority order):
// 1. Partitioned tables (PG 10+): check partition key for date type
// 2. Scan columns for common date column names
// 3. First date/timestamp column as last resort
func (p *Postgres) DetectDateColumns(s *schema.Schema) (map[string]string, error) {
	result := make(map[string]string)

	// Try to detect partition keys for partitioned tables
	partitionKeys, err := p.getPartitionKeys()
	if err != nil {
		log.WithField("error", err).Debug("failed to query partition keys, skipping")
	}

	for _, table := range s.Tables {
		// Skip views
		if table.Type == "VIEW" || table.Type == "MATERIALIZED VIEW" {
			continue
		}

		// Strategy 1: Check partition key
		if partitionKeys != nil {
			if partKey, ok := partitionKeys[table.Name]; ok {
				// Verify the partition key column is a date type
				for _, col := range table.Columns {
					if col.Name == partKey && isDateType(col.Type) {
						result[table.Name] = partKey
						log.WithFields(logrus.Fields{
							"table":  table.Name,
							"column": partKey,
							"source": "partition_key",
						}).Debug("detected date column")
						break
					}
				}
				if _, found := result[table.Name]; found {
					continue
				}
			}
		}

		// Strategy 2: Look for common date column names
		found := false
		for _, commonName := range commonDateColumnNames {
			for _, col := range table.Columns {
				if strings.ToLower(col.Name) == commonName && isDateType(col.Type) {
					result[table.Name] = col.Name
					log.WithFields(logrus.Fields{
						"table":  table.Name,
						"column": col.Name,
						"source": "common_name",
					}).Debug("detected date column")
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if found {
			continue
		}

		// Strategy 3: First date/timestamp column as last resort
		for _, col := range table.Columns {
			if isDateType(col.Type) {
				result[table.Name] = col.Name
				log.WithFields(logrus.Fields{
					"table":  table.Name,
					"column": col.Name,
					"source": "first_date_column",
				}).Debug("detected date column")
				break
			}
		}
	}

	return result, nil
}

// getPartitionKeys queries pg_partitioned_table for partition key columns (PG 10+)
func (p *Postgres) getPartitionKeys() (map[string]string, error) {
	query := `
		SELECT
			n.nspname || '.' || c.relname AS table_name,
			a.attname AS partition_key
		FROM pg_partitioned_table pt
		JOIN pg_class c ON c.oid = pt.partrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		JOIN pg_attribute a ON a.attrelid = pt.partrelid
			AND a.attnum = ANY(pt.partattrs::smallint[])
		WHERE n.nspname NOT IN ('pg_catalog', 'information_schema')
		AND array_length(pt.partattrs::smallint[], 1) = 1
	`

	rows, err := p.db.Query(query)
	if err != nil {
		// pg_partitioned_table doesn't exist in PG < 10
		return nil, fmt.Errorf("partition key query failed: %w", err)
	}
	defer rows.Close()

	result := make(map[string]string)
	for rows.Next() {
		var tableName, partKey string
		if err := rows.Scan(&tableName, &partKey); err != nil {
			continue
		}
		result[tableName] = partKey
	}

	return result, nil
}
