package drivers

import (
	"github.com/k1LoW/tbls/schema"
)

// Driver is the common interface for database drivers.
type Driver interface {
	Analyze(*schema.Schema) error
	Info() (*schema.Driver, error)
}

// StatsCollector is an optional interface for drivers that support statistics collection
type StatsCollector interface {
	CollectStats(s *schema.Schema, cfg StatsConfig) error
}

// StatsConfig is passed to StatsCollector
type StatsConfig struct {
	Include             []string
	Exclude             []string
	TopN                int
	SampleSize          int
	LargeTableThreshold int64
	RecentDays          int
}

// Option is the type for change Config.
type Option func(Driver) error
