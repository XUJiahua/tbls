package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	wildcard "github.com/IGLOU-EU/go-wildcard/v2"
	"github.com/aquasecurity/go-version/pkg/version"
	"github.com/goccy/go-yaml"
	"github.com/k1LoW/errors"
	"github.com/k1LoW/expand"
	"github.com/k1LoW/tbls/dict"
	"github.com/k1LoW/tbls/schema"
	ver "github.com/k1LoW/tbls/version"
	"github.com/samber/lo"
)

const DefaultDocPath = "dbdoc"

var DefaultConfigFilePaths = []string{".tbls.yml", "tbls.yml", ".tbls.yaml", "tbls.yaml"}

// DefaultERFormat is the default ER diagram format.
const DefaultERFormat = "svg"

var SupportERFormat = []string{"png", "jpg", "svg", "mermaid"}

const SchemaFileName = "schema.json"

// DefaultERDistance is the default distance between tables that display relations in the ER.
var DefaultERDistance = 1

// Config is tbls config.
type Config struct {
	Name   string   `yaml:"name" json:"name"`
	Desc   string   `yaml:"desc,omitempty" json:"desc,omitempty"`
	Labels []string `yaml:"labels,omitempty" json:"labels,omitempty"`
	DSN    DSN      `yaml:"dsn" json:"dsn"`
	// Directory of schema document
	DocPath                string                 `yaml:"docPath" json:"docPath,omitempty"`
	Format                 Format                 `yaml:"format,omitempty" json:"format,omitempty"`
	ER                     ER                     `yaml:"er,omitempty" json:"er,omitempty"`
	Include                []string               `yaml:"include,omitempty" json:"include,omitempty"`
	Exclude                []string               `yaml:"exclude,omitempty" json:"exclude,omitempty"`
	Distance               int                    `yaml:"distance,omitempty" json:"distance,omitempty"`
	Dict                   dict.Dict              `yaml:"dict,omitempty" json:"dict,omitempty"`
	Templates              Templates              `yaml:"templates,omitempty" json:"templates,omitempty"`
	DetectVirtualRelations DetectVirtualRelations `yaml:"detectVirtualRelations,omitempty" json:"detectVirtualRelations,omitempty"`
	BaseURL                string                 `yaml:"baseUrl,omitempty" json:"baseUrl,omitempty"`
	RequiredVersion        string                 `yaml:"requiredVersion,omitempty" json:"requiredVersion,omitempty"`
	Stats                  StatsConfig            `yaml:"stats,omitempty" json:"stats,omitempty"`
	MergedDict             dict.Dict              `yaml:"-" json:"-"`

	// Table labels to be included
	includeLabels []string

	// Path of config file
	Path string `yaml:"-" json:"-"`
	root string `yaml:"-" json:"-"`
}

type DSN struct {
	URL     string            `yaml:"url" json:"url"`
	Headers map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`
}

// Format is document format setting.
type Format struct {
	Adjust                   bool     `yaml:"adjust,omitempty" json:"adjust,omitempty"`
	Sort                     bool     `yaml:"sort,omitempty" json:"sort,omitempty"`
	Number                   bool     `yaml:"number,omitempty" json:"number,omitempty"`
	ShowOnlyFirstParagraph   bool     `yaml:"showOnlyFirstParagraph,omitempty" json:"showOnlyFirstParagraph,omitempty"`
	HideColumnsWithoutValues []string `yaml:"hideColumnsWithoutValues,omitempty" json:"hideColumnsWithoutValues,omitempty"`
}

// ER is er setting.
type ER struct {
	Skip            bool             `yaml:"skip,omitempty" json:"skip,omitempty"`
	Format          string           `yaml:"format,omitempty" json:"format,omitempty"`
	Comment         bool             `yaml:"comment,omitempty" json:"comment,omitempty"`
	HideDef         bool             `yaml:"hideDef,omitempty" json:"hideDef,omitempty"`
	ShowColumnTypes *ShowColumnTypes `yaml:"showColumnTypes,omitempty" json:"showColumnTypes,omitempty"`
	Distance        *int             `yaml:"distance,omitempty" json:"distance,omitempty"`
	Font            string           `yaml:"font,omitempty" json:"font,omitempty"`
}

// ShowColumnTypes is show column setting for ER diagram.
type ShowColumnTypes struct {
	Related bool `yaml:"related,omitempty" json:"related,omitempty"`
	Primary bool `yaml:"primary,omitempty" json:"primary,omitempty"`
}

type DetectVirtualRelations struct {
	Enabled  bool   `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Strategy string `yaml:"strategy,omitempty" json:"strategy,omitempty"`
}

// StatsConfig holds configuration for statistics collection
type StatsConfig struct {
	Enabled             bool                        `yaml:"enabled" json:"enabled"`
	Include             []string                    `yaml:"include,omitempty" json:"include,omitempty"`
	Exclude             []string                    `yaml:"exclude,omitempty" json:"exclude,omitempty"`
	TopN                int                         `yaml:"topN,omitempty" json:"topN,omitempty"`
	SampleSize          int                         `yaml:"sampleSize,omitempty" json:"sampleSize,omitempty"`
	LargeTableThreshold int64                       `yaml:"largeTableThreshold,omitempty" json:"largeTableThreshold,omitempty"`
	RecentDays          int                         `yaml:"recentDays,omitempty" json:"recentDays,omitempty"`
	Inference           InferenceConfig             `yaml:"inference,omitempty" json:"inference,omitempty"`
	Checkpoint          CheckpointConfig            `yaml:"checkpoint,omitempty" json:"checkpoint,omitempty"`
	DateColumn          string                      `yaml:"dateColumn,omitempty" json:"dateColumn,omitempty"`
	Tables              map[string]TableStatsConfig `yaml:"tables,omitempty" json:"tables,omitempty"`
}

// TableStatsConfig holds per-table stats configuration
type TableStatsConfig struct {
	DateColumn string `yaml:"dateColumn,omitempty" json:"dateColumn,omitempty"`
	Skip       bool   `yaml:"skip,omitempty" json:"skip,omitempty"`
}

// CheckpointConfig holds configuration for checkpoint/resume functionality
type CheckpointConfig struct {
	Enabled bool   `yaml:"enabled" json:"enabled"`
	TTL     string `yaml:"ttl,omitempty" json:"ttl,omitempty"` // Duration string like "24h"
	Force   bool   `yaml:"force,omitempty" json:"force,omitempty"`
}

// UnmarshalYAML supports both `checkpoint: true` and `checkpoint: {enabled: true, ...}`
func (c *CheckpointConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	// Try boolean first
	var boolVal bool
	if err := unmarshal(&boolVal); err == nil {
		c.Enabled = boolVal
		return nil
	}

	// Try object
	type plain CheckpointConfig
	return unmarshal((*plain)(c))
}

// DefaultCheckpointConfig returns default checkpoint configuration
func DefaultCheckpointConfig() CheckpointConfig {
	return CheckpointConfig{
		Enabled: true,
		TTL:     "24h",
		Force:   false,
	}
}

// InferenceConfig holds configuration for stats-based inference
type InferenceConfig struct {
	Enabled                 bool    `yaml:"enabled" json:"enabled"`
	EnumMaxCardinality      float64 `yaml:"enumMaxCardinality,omitempty" json:"enumMaxCardinality,omitempty"`
	EnumMaxDistinct         int     `yaml:"enumMaxDistinct,omitempty" json:"enumMaxDistinct,omitempty"`
	DictMaxCardinality      float64 `yaml:"dictMaxCardinality,omitempty" json:"dictMaxCardinality,omitempty"`
	DictMaxDistinct         int     `yaml:"dictMaxDistinct,omitempty" json:"dictMaxDistinct,omitempty"`
	ForeignKeyMinConfidence float64 `yaml:"foreignKeyMinConfidence,omitempty" json:"foreignKeyMinConfidence,omitempty"`
}

// UnmarshalYAML supports both `inference: true` and `inference: {enabled: true, ...}`
func (c *InferenceConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	// Try boolean first
	var boolVal bool
	if err := unmarshal(&boolVal); err == nil {
		c.Enabled = boolVal
		return nil
	}

	// Try object
	type plain InferenceConfig
	return unmarshal((*plain)(c))
}

// DefaultInferenceConfig returns default inference configuration
func DefaultInferenceConfig() InferenceConfig {
	return InferenceConfig{
		Enabled:                 false,
		EnumMaxCardinality:      0.01,
		EnumMaxDistinct:         20,
		DictMaxCardinality:      0.05,
		DictMaxDistinct:         100,
		ForeignKeyMinConfidence: 0.7,
	}
}

// Option function change Config.
type Option func(*Config) error

// DSNURL return Option set Config.DSN.URL.
func DSNURL(dsn string) Option {
	return func(c *Config) error {
		c.DSN.URL = dsn
		return nil
	}
}

// DocPath return Option set Config.DocPath.
func DocPath(docPath string) Option {
	return func(c *Config) error {
		c.DocPath = docPath
		return nil
	}
}

// Adjust return Option set Config.Format.Adjust.
func Adjust(adjust bool) Option {
	return func(c *Config) error {
		if adjust {
			c.Format.Adjust = adjust
		}
		return nil
	}
}

// Sort return Option set Config.Format.Sort.
func Sort(sort bool) Option {
	return func(c *Config) error {
		if sort {
			c.Format.Sort = sort
		}
		return nil
	}
}

// ERSkip return Option set Config.ER.Skip.
func ERSkip(skip bool) Option {
	return func(c *Config) error {
		c.ER.Skip = skip
		return nil
	}
}

// ERFormat return Option set Config.ER.Format.
func ERFormat(erFormat string) Option {
	return func(c *Config) error {
		if erFormat != "" {
			c.ER.Format = erFormat
		}
		return nil
	}
}

// Distance return Option set Config.Distance.
func Distance(distance int) Option {
	return func(c *Config) error {
		c.Distance = distance
		return nil
	}
}

// BaseURL return Option set Config.BaseURL.
func BaseURL(baseURL string) Option {
	return func(c *Config) error {
		if baseURL != "" {
			c.BaseURL = baseURL
		}
		return nil
	}
}

// Include return Option set Config.Include.
func Include(i []string) Option {
	return func(c *Config) error {
		if len(i) > 0 {
			c.Include = i
		}
		return nil
	}
}

// Exclude return Option set Config.Exclude.
func Exclude(e []string) Option {
	return func(c *Config) error {
		if len(e) > 0 {
			c.Exclude = e
		}
		return nil
	}
}

// IncludeLabels return Option set Config.includeLabels.
func IncludeLabels(l []string) Option {
	return func(c *Config) error {
		if len(l) > 0 {
			c.includeLabels = l
		}
		return nil
	}
}

// New return Config.
func New() (*Config, error) {
	c := Config{}
	err := c.setDefault()
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// Load config with all method.
func (c *Config) Load(configPath string, options ...Option) error {
	if err := c.LoadConfigFile(configPath); err != nil {
		return err
	}

	if err := c.LoadEnviron(); err != nil {
		return err
	}

	if err := c.LoadOption(options...); err != nil {
		return err
	}

	if err := c.setDefault(); err != nil {
		return err
	}

	if err := c.validate(); err != nil {
		return err
	}

	return nil
}

// LoadOptions load options.
func (c *Config) LoadOption(options ...Option) error {
	for _, option := range options {
		if err := option(c); err != nil {
			return err
		}
	}
	return nil
}

// set default setting.
func (c *Config) setDefault() error {
	if c.DocPath == "" {
		c.DocPath = DefaultDocPath
	}

	if c.ER.Format == "" {
		c.ER.Format = DefaultERFormat
	}

	if c.ER.Distance == nil {
		c.ER.Distance = &DefaultERDistance
	}

	// Stats defaults
	if c.Stats.TopN == 0 {
		c.Stats.TopN = 10
	}
	if c.Stats.SampleSize == 0 {
		c.Stats.SampleSize = 10000
	}
	if c.Stats.LargeTableThreshold == 0 {
		c.Stats.LargeTableThreshold = 1000000
	}
	if c.Stats.RecentDays == 0 {
		c.Stats.RecentDays = 30
	}

	// Inference defaults (only apply when inference is enabled)
	if c.Stats.Inference.Enabled {
		defaults := DefaultInferenceConfig()
		if c.Stats.Inference.EnumMaxCardinality == 0 {
			c.Stats.Inference.EnumMaxCardinality = defaults.EnumMaxCardinality
		}
		if c.Stats.Inference.EnumMaxDistinct == 0 {
			c.Stats.Inference.EnumMaxDistinct = defaults.EnumMaxDistinct
		}
		if c.Stats.Inference.DictMaxCardinality == 0 {
			c.Stats.Inference.DictMaxCardinality = defaults.DictMaxCardinality
		}
		if c.Stats.Inference.DictMaxDistinct == 0 {
			c.Stats.Inference.DictMaxDistinct = defaults.DictMaxDistinct
		}
		if c.Stats.Inference.ForeignKeyMinConfidence == 0 {
			c.Stats.Inference.ForeignKeyMinConfidence = defaults.ForeignKeyMinConfidence
		}
	}

	// Checkpoint defaults (checkpoint is enabled by default when stats is enabled)
	if c.Stats.Enabled {
		checkpointDefaults := DefaultCheckpointConfig()
		// If checkpoint config was not explicitly set, use defaults
		if c.Stats.Checkpoint.TTL == "" {
			c.Stats.Checkpoint.TTL = checkpointDefaults.TTL
		}
		// Enabled defaults to true unless explicitly disabled
		// Note: This is handled by the zero value being false, so we check if it wasn't set
		// In YAML, if checkpoint is not specified at all, we default to enabled
		if !c.Stats.Checkpoint.Force && c.Stats.Checkpoint.TTL == checkpointDefaults.TTL {
			c.Stats.Checkpoint.Enabled = true
		}
	}

	return nil
}

func (c *Config) checkVersion(sv string) error {
	if sv == "dev" {
		return nil
	}
	if c.RequiredVersion == "" {
		return nil
	}
	cons, err := version.NewConstraints(c.RequiredVersion)
	if err != nil {
		return err
	}
	v, err := version.Parse(sv)
	if err != nil {
		return err
	}
	if !cons.Check(v) {
		return fmt.Errorf("the required tbls version for the configuration is '%s'. however, the running tbls version is '%s'", c.RequiredVersion, sv)
	}

	return nil
}

func (c *Config) validate() error {
	if err := c.checkVersion(ver.Version); err != nil {
		return err
	}
	if !lo.Contains(SupportERFormat, c.ER.Format) {
		return fmt.Errorf("unsupported ER format: %s", c.ER.Format)
	}

	return nil
}

// LoadEnviron load environment variables.
func (c *Config) LoadEnviron() error {
	dsn := os.Getenv("TBLS_DSN")
	if dsn != "" {
		c.DSN.URL = dsn
	}
	docPath := os.Getenv("TBLS_DOC_PATH")
	if docPath != "" {
		c.DocPath = docPath
	}
	return nil
}

// LoadConfigFile load config file.
func (c *Config) LoadConfigFile(path string) (err error) {
	defer func() {
		err = errors.WithStack(err)
	}()
	if path == "" && os.Getenv("TBLS_DSN") == "" {
		var paths []string
		for _, p := range DefaultConfigFilePaths {
			if f, err := os.Stat(filepath.Join(c.root, p)); err == nil && !f.IsDir() {
				paths = append(paths, p)
			}
		}
		if len(paths) == 0 {
			return nil
		}
		if len(paths) > 1 {
			return fmt.Errorf("duplicate config file [%s]", strings.Join(paths, ", "))
		}
		path = paths[0]
	}

	if path == "" {
		return nil
	}

	fullPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("failed to load config file: %w", err)
	}

	buf, err := os.ReadFile(filepath.Clean(fullPath))
	if err != nil {
		return fmt.Errorf("failed to load config file: %w", err)
	}
	c.Path = filepath.Clean(fullPath)

	return c.LoadConfig(buf)
}

// LoadConfig load config from []byte.
func (c *Config) LoadConfig(in []byte) (err error) {
	defer func() {
		err = errors.WithStack(err)
	}()
	if err := yaml.Unmarshal(expand.ExpandenvYAMLBytes(in), c); err != nil {
		return fmt.Errorf("failed to load config file: %w", err)
	}
	c.MergedDict.Merge(c.Dict.Dump())
	return nil
}

// ModifySchema modify schema.Schema by config.
func (c *Config) ModifySchema(s *schema.Schema) error {
	if c.Name != "" {
		s.Name = c.Name
	}
	if c.Desc != "" {
		s.Desc = c.Desc
	}
	// set Labels
	for _, l := range c.Labels {
		s.Labels = s.Labels.Merge(l)
	}
	if err := detectPKFK(s); err != nil {
		return err
	}
	if err := c.FilterTables(s); err != nil {
		return err
	}
	if c.Format.Sort {
		if err := s.Sort(); err != nil {
			return err
		}
	}
	if c.DetectVirtualRelations.Enabled {
		strategy, err := SelectNamingStrategy(c.DetectVirtualRelations.Strategy)
		if err != nil {
			return err
		}
		MergeDetectedRelations(s, strategy)
	}
	c.mergeDictFromSchema(s)
	if err := detectCardinality(s); err != nil {
		return err
	}
	if err := c.detectShowColumnsForER(s); err != nil {
		return err
	}

	return nil
}

// FilterTables filter tables from schema.Schema using include: and exclude: and includeLabels.
func (c *Config) FilterTables(s *schema.Schema) error {
	return s.Filter(&schema.FilterOption{
		Include:       c.Include,
		Exclude:       c.Exclude,
		IncludeLabels: c.includeLabels,
		Distance:      c.Distance,
	})
}

// ShouldCollectStats returns true if stats should be collected for the given table
func (c *Config) ShouldCollectStats(tableName string) bool {
	if !c.Stats.Enabled {
		return false
	}
	// Check exclude first
	if len(c.Stats.Exclude) > 0 && match(c.Stats.Exclude, tableName) {
		return false
	}
	// If include is specified, table must match
	if len(c.Stats.Include) > 0 {
		return match(c.Stats.Include, tableName)
	}
	// Default: collect for all tables
	return true
}

func (c *Config) mergeDictFromSchema(s *schema.Schema) {
	if s.Driver != nil && s.Driver.Meta != nil && s.Driver.Meta.Dict != nil {
		c.MergedDict.Merge(s.Driver.Meta.Dict.Dump())
	}
}

// MaskedDSN return DSN mask password.
func (c *Config) MaskedDSN() (string, error) {
	u, err := url.Parse(c.DSN.URL)
	if err != nil {
		return c.DSN.URL, errors.WithStack(err)
	}
	_, pset := u.User.Password()
	if !pset {
		return c.DSN.URL, nil
	}
	tmp := "-----tbls-----"
	u.User = url.UserPassword(u.User.Username(), tmp)
	return strings.Replace(u.String(), tmp, "*****", 1), nil
}

func (c *Config) SchemaFilePath() string {
	return filepath.Join(c.DocPath, SchemaFileName)
}

func (c *Config) NeedToGenerateERImages() bool {
	if c.ER.Skip {
		return false
	}
	if c.ER.Format == "mermaid" {
		return false
	}
	return true
}

func (c *Config) detectShowColumnsForER(s *schema.Schema) error {
	if c.ER.ShowColumnTypes == nil {
		return nil
	}

	if !c.ER.ShowColumnTypes.Related && !c.ER.ShowColumnTypes.Primary {
		return errors.New("er.showColumnTypes: must be true at least one")
	}

	for _, t := range s.Tables {
		for _, cc := range t.Columns {
			if c.ER.ShowColumnTypes.Related && (len(cc.ChildRelations) > 0 || len(cc.ParentRelations) > 0) {
				// related
				cc.HideForER = false
			} else if c.ER.ShowColumnTypes.Primary && cc.PK {
				// primary
				cc.HideForER = false
			} else {
				cc.HideForER = true
				for _, r := range cc.ChildRelations {
					r.HideForER = true
				}
				for _, r := range cc.ParentRelations {
					r.HideForER = true
				}
			}
		}
	}

	return nil
}

// MergeDetectedRelations detects and merges virtual relations based on naming conventions
func MergeDetectedRelations(s *schema.Schema, strategy *NamingStrategy) {
	var (
		err          error
		parentColumn *schema.Column
		parentTable  *schema.Table
	)

	for _, t := range s.Tables {
		for _, c := range t.Columns {
			relation := &schema.Relation{
				Virtual: true,
				Def:     "Detected Relation",
				Table:   t,
			}

			if parentTable, err = s.FindTableByName(strategy.ParentTableName(c.Name)); err != nil {
				continue
			}

			if parentTable == t {
				continue
			}

			relation.ParentTable = parentTable

			if parentColumn, err = relation.ParentTable.FindColumnByName(strategy.ParentColumnName(c.Name)); err != nil {
				continue
			}

			relation.Columns = append(relation.Columns, c)
			relation.ParentColumns = append(relation.ParentColumns, parentColumn)

			if _, err := s.FindRelation(relation.Columns, relation.ParentColumns); err == nil {
				// If the relation already exists, do not create a new virtual relation.
				continue
			}

			c.ParentRelations = append(c.ParentRelations, relation)
			parentColumn.ChildRelations = append(parentColumn.ChildRelations, relation)
			s.Relations = append(s.Relations, relation)
		}
	}
}

func matchLength(s []string, e string) (int, bool) {
	for _, v := range s {
		if wildcard.Match(v, e) {
			return len(strings.ReplaceAll(v, "*", "")), true
		}
	}
	return 0, false
}

// This function should be applied to the completed schema.
func detectCardinality(s *schema.Schema) error {
	for _, r := range s.Relations {
		// child
		if r.Cardinality == schema.UnknownCardinality {
			unique := false
			columns := []string{}
			for _, c := range r.Columns {
				columns = append(columns, c.Name)
			}
		LL:
			for _, c := range r.Table.Constraints {
				if len(columns) != len(c.Columns) {
					continue
				}
				for _, cc := range c.Columns {
					if !lo.Contains(columns, cc) {
						continue LL
					}
				}
				if strings.Contains(strings.ToUpper(c.Def), "UNIQUE") || strings.Contains(strings.ToUpper(c.Def), "PRIMARY KEY") {
					unique = true
				}
			}
			if unique {
				r.Cardinality = schema.ZeroOrOne
			} else {
				r.Cardinality = schema.ZeroOrMore
			}
		}

		// parent
		if r.ParentCardinality == schema.UnknownCardinality {
			// whether the child columns are nullable or not.
			nullable := true
			for _, c := range r.Columns {
				if !c.Nullable {
					nullable = false
				}
			}

			if nullable {
				r.ParentCardinality = schema.ZeroOrOne
			} else {
				r.ParentCardinality = schema.ExactlyOne
			}
		}
	}
	return nil
}

func detectPKFK(s *schema.Schema) error {
	for _, t := range s.Tables {
		// PRIMARY KEY
		for _, i := range t.Indexes {
			if !strings.Contains(i.Def, "PRIMARY") {
				continue
			}
			for _, c := range i.Columns {
				column, err := t.FindColumnByName(c)
				if err != nil {
					return err
				}
				column.PK = true
			}
		}
		// Foreign Key (Relations)
		for _, c := range t.Columns {
			if len(c.ParentRelations) > 0 && !c.PK {
				c.FK = true
			}
		}
	}
	return nil
}

func match(s []string, e string) bool {
	_, m := matchLength(s, e)
	return m
}
