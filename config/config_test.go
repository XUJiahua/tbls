package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/k1LoW/tbls/schema"
)

func TestLoadDefault(t *testing.T) {
	configFilepath := filepath.Join(testdataDir(), "empty.yml")
	config, err := New()
	if err != nil {
		t.Fatal(err)
	}
	err = config.Load(configFilepath)
	if err != nil {
		t.Fatal(err)
	}

	if want := ""; config.DSN.URL != want {
		t.Errorf("got %v\nwant %v", config.DSN.URL, want)
	}
	if want := "dbdoc"; config.DocPath != want {
		t.Errorf("got %v\nwant %v", config.DocPath, want)
	}
	if want := "svg"; config.ER.Format != want {
		t.Errorf("got %v\nwant %v", config.ER.Format, want)
	}
	if want := 1; *config.ER.Distance != want {
		t.Errorf("got %v\nwant %v", config.ER.Distance, want)
	}
}

func TestLoadConfigFile(t *testing.T) {
	t.Setenv("TBLS_TEST_PG_PASS", "pgpass")
	t.Setenv("TBLS_TEST_PG_DOC_PATH", "sample/pg")
	configFilepath := filepath.Join(testdataDir(), "config_test_tbls_2.yml")
	config, err := New()
	if err != nil {
		t.Fatal(err)
	}
	err = config.LoadConfigFile(configFilepath)
	if err != nil {
		t.Fatal(err)
	}

	if want := "pg://root:pgpass@localhost:55432/testdb?sslmode=disable"; config.DSN.URL != want {
		t.Errorf("got %v\nwant %v", config.DSN.URL, want)
	}

	if want := "sample/pg"; config.DocPath != want {
		t.Errorf("got %v\nwant %v", config.DocPath, want)
	}

	if want := "INDEX"; config.MergedDict.Lookup("Indexes") != want {
		t.Errorf("got %v\nwant %v", config.MergedDict.Lookup("Indexes"), want)
	}
}

func TestDuplicateConfigFile(t *testing.T) {
	config := &Config{
		root: filepath.Join(testdataDir(), "config"),
	}
	got := config.LoadConfigFile("")
	want := "duplicate config file [.tbls.yml, tbls.yml, .tbls.yaml, tbls.yaml]"
	if fmt.Sprintf("%v", got) != want {
		t.Errorf("got %v\nwant %v", got, want)
	}
}


func TestFilterTables(t *testing.T) {
	c, err := New()
	if err != nil {
		t.Error(err)
	}
	tests := []struct {
		include       []string
		exclude       []string
		labels        []string
		distance      int
		wantTables    int
		wantRelations int
	}{
		{[]string{}, []string{}, []string{}, 0, 5, 3},
		{[]string{}, []string{"schema_migrations"}, []string{}, 0, 4, 3},
		{[]string{}, []string{"users"}, []string{}, 0, 4, 1},
		{[]string{"users"}, []string{}, []string{}, 0, 1, 0},
		{[]string{"user*"}, []string{}, []string{}, 0, 2, 1},
		{[]string{"*options"}, []string{}, []string{}, 0, 1, 0},
		{[]string{"*"}, []string{"user_options"}, []string{}, 0, 4, 2},
		{[]string{"not_exist"}, []string{}, []string{}, 0, 0, 0},
		{[]string{"not_exist", "*"}, []string{}, []string{}, 0, 5, 3},
		{[]string{"users"}, []string{"*"}, []string{}, 0, 1, 0},
		{[]string{"use*"}, []string{"use*"}, []string{}, 0, 2, 1},
		{[]string{"use*"}, []string{"user*"}, []string{}, 0, 0, 0},
		{[]string{"user*"}, []string{"user_*"}, []string{}, 0, 1, 0},
		{[]string{"*", "user*"}, []string{"user_*"}, []string{}, 0, 4, 2},

		{[]string{"users"}, []string{}, []string{}, 1, 3, 2},
		{[]string{"user_options"}, []string{}, []string{}, 1, 2, 1},
		{[]string{"user_options"}, []string{}, []string{}, 2, 3, 2},
		{[]string{"user_options"}, []string{}, []string{}, 3, 4, 3},
		{[]string{}, []string{}, []string{}, 9, 5, 3},
		{[]string{"posts"}, []string{}, []string{}, 9, 4, 3},
		{[]string{""}, []string{"*"}, []string{}, 9, 0, 0},

		{[]string{}, []string{}, []string{"private"}, 0, 2, 1},
		{[]string{}, []string{}, []string{"option"}, 0, 2, 0},
		{[]string{}, []string{}, []string{"public", "private"}, 0, 4, 3},
		{[]string{}, []string{"users"}, []string{"private"}, 0, 1, 0},
		{[]string{}, []string{"user*"}, []string{"option"}, 0, 1, 0},
		{[]string{"users"}, []string{}, []string{"private"}, 0, 2, 1},
		{[]string{}, []string{}, []string{"p*"}, 0, 4, 3},
		{[]string{"users"}, []string{}, []string{"pri*"}, 0, 2, 1},
	}
	for i, tt := range tests {
		t.Run(fmt.Sprintf("%d.%v%v%v", i, tt.include, tt.exclude, tt.labels), func(t *testing.T) {
			s := newTestSchemaViaJSON(t)
			c.Include = tt.include
			c.Exclude = tt.exclude
			c.includeLabels = tt.labels
			c.Distance = tt.distance
			err = c.FilterTables(s)
			if err != nil {
				t.Error(err)
			}
			if got := len(s.Tables); got != tt.wantTables {
				t.Errorf("got %v\nwant %v", got, tt.wantTables)
			}
			if got := len(s.Relations); got != tt.wantRelations {
				t.Errorf("got %v\nwant %v", got, tt.wantRelations)
			}
		})
	}
}


func TestMaskedDSN(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{
			"pg://root:pgpass@localhost:5432/testdb?sslmode=disable",
			"pg://root:*****@localhost:5432/testdb?sslmode=disable",
		},
		{
			"pg://root@localhost:5432/testdb?sslmode=disable",
			"pg://root@localhost:5432/testdb?sslmode=disable",
		},
		{
			"pg://localhost:5432/testdb?sslmode=disable",
			"pg://localhost:5432/testdb?sslmode=disable",
		},
		{
			"bq://project-id/dataset-id?creds=/path/to/google_application_credentials.json",
			"bq://project-id/dataset-id?creds=/path/to/google_application_credentials.json",
		},
	}

	for _, tt := range tests {
		config, err := New()
		if err != nil {
			t.Fatal(err)
		}
		config.DSN.URL = tt.url
		got, err := config.MaskedDSN()
		if err != nil {
			t.Fatal(err)
		}
		if got != tt.want {
			t.Errorf("got %v\nwant %v", got, tt.want)
		}
	}
}

func testdataDir() string {
	wd, _ := os.Getwd()
	dir, _ := filepath.Abs(filepath.Join(filepath.Dir(wd), "testdata"))
	return dir
}

func TestMergeDetectedRelations(t *testing.T) {
	var (
		err          error
		table        *schema.Table
		column       *schema.Column
		parentColumn *schema.Column
		relations    []*schema.Relation
	)
	s1 := &schema.Schema{
		Name: "testschema",
		Tables: []*schema.Table{
			{
				Name:    "users",
				Comment: "users comment",
				Columns: []*schema.Column{
					{
						Name: "id",
						Type: "serial",
					},
					{
						Name: "username",
						Type: "text",
					},
				},
			},
			{
				Name:    "posts",
				Comment: "posts comment",
				Columns: []*schema.Column{
					{
						Name: "id",
						Type: "serial",
					},
					{
						Name: "user_id",
						Type: "int",
					},
					{
						Name: "title",
						Type: "text",
					},
				},
			},
		},
	}
	s2 := &schema.Schema{
		Name: "testschema",
		Tables: []*schema.Table{
			{
				Name:    "users",
				Comment: "users comment",
				Columns: []*schema.Column{
					{
						Name: "id",
						Type: "serial",
					},
				},
			},
			{
				Name:    "posts",
				Comment: "posts comment",
				Columns: []*schema.Column{
					{
						Name: "id",
						Type: "serial",
					},
					{
						Name: "uid",
						Type: "int",
					},
					{
						Name: "title",
						Type: "text",
					},
				},
			},
		},
	}
	table, err = s1.FindTableByName("posts")
	if err != nil {
		t.Fatal(err)
	}
	column, err = table.FindColumnByName("user_id")
	if err != nil {
		t.Fatal(err)
	}

	relation := &schema.Relation{
		Virtual: true,
		Def:     "Detected Relation",
		Table:   table,
	}
	strategy, err := SelectNamingStrategy("default")
	if err != nil {
		t.Fatal(err)
	}
	if relation.ParentTable, err = s1.FindTableByName(strategy.ParentTableName("user_id")); err != nil {
		t.Fatal(err)
	}
	if parentColumn, err = relation.ParentTable.FindColumnByName(strategy.ParentColumnName("users")); err != nil {
		t.Fatal(err)
	}
	relation.Columns = append(relation.Columns, column)
	relation.ParentColumns = append(relation.ParentColumns, parentColumn)

	column.ParentRelations = append(column.ParentRelations, relation)
	parentColumn.ChildRelations = append(parentColumn.ChildRelations, relation)

	relations = append(relations, relation)

	type args struct {
		s *schema.Schema
	}
	type want struct {
		r []*schema.Relation
	}
	tests := []struct {
		name string
		args args
		want want
	}{
		{
			name: "Detect relation succeed",
			args: args{
				s: s1,
			},
			want: want{
				r: relations,
			},
		},
		{
			name: "Detect relation failed",
			args: args{
				s: s2,
			},
			want: want{
				r: nil,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			MergeDetectedRelations(tt.args.s, strategy)
			if !reflect.DeepEqual(tt.args.s.Relations, tt.want.r) {
				t.Errorf("got: %#v\nwant: %#v", tt.args.s.Relations, tt.want.r)
			}
		})
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		erFormat string
		wantErr  bool
	}{
		{"", true},
		{"png", false},
		{"mermaid", false},
		{"invalid", true},
	}
	for _, tt := range tests {
		t.Run(tt.erFormat, func(t *testing.T) {
			c, err := New()
			if err != nil {
				t.Fatal(err)
			}
			c.ER.Format = tt.erFormat
			if err := c.validate(); err != nil {
				if !tt.wantErr {
					t.Errorf("got error: %s", err)
				}
				return
			}
			if tt.wantErr {
				t.Error("want error")
			}
		})
	}
}

func TestCheckVersion(t *testing.T) {
	tests := []struct {
		v    string
		c    string
		want error
	}{
		{"1.42.3", ">= 1.42", nil},
		{"1.42.3", "", nil},
		{"1.42.3", ">= 1.42, < 2", nil},
		{"1.42.3", "> 1.42", nil},
		{"1.42.3", "1.42.3", nil},
		{"1.42.3", "1.42.4", errors.New("the required tbls version for the configuration is '1.42.4'. however, the running tbls version is '1.42.3'")},
	}
	for _, tt := range tests {
		cfg, err := New()
		if err != nil {
			t.Fatal(err)
		}
		cfg.RequiredVersion = tt.c
		if got := cfg.checkVersion(tt.v); fmt.Sprintf("%s", got) != fmt.Sprintf("%s", tt.want) {
			t.Errorf("got %v\nwant %v", got, tt.want)
		}
	}
}

func TestNeedToGenerateERImages(t *testing.T) {
	tests := []struct {
		c    *Config
		want bool
	}{
		{&Config{ER: ER{Skip: true}}, false},
		{&Config{ER: ER{Format: "png"}}, true},
		{&Config{ER: ER{Format: "mermaid"}}, false},
	}
	for i, tt := range tests {
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			got := tt.c.NeedToGenerateERImages()
			if got != tt.want {
				t.Errorf("got %v\nwant %v", got, tt.want)
			}
		})
	}
}

func TestDetectShowColumnsForER(t *testing.T) {
	tests := []struct {
		showColumnTypes   *ShowColumnTypes
		wantColumnCount   int
		wantRelationCount int
	}{
		{nil, 13, 3},
		{&ShowColumnTypes{Related: true, Primary: false}, 5, 3},
		{&ShowColumnTypes{Related: false, Primary: true}, 0, 0},
		{&ShowColumnTypes{Related: true, Primary: true}, 5, 3},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%v", tt.showColumnTypes), func(t *testing.T) {
			c, err := New()
			if err != nil {
				t.Fatal(err)
			}
			c.ER.ShowColumnTypes = tt.showColumnTypes
			s := newTestSchemaViaJSON(t)
			if err := c.ModifySchema(s); err != nil {
				t.Fatal(err)
			}
			var (
				gotColumnCount   int
				gotRelationCount int
			)
			for _, tt := range s.Tables {
				for _, cc := range tt.Columns {
					if !cc.HideForER {
						gotColumnCount++
					}
				}
			}
			for _, r := range s.Relations {
				if !r.HideForER {
					gotRelationCount++
				}
			}
			if gotColumnCount != tt.wantColumnCount {
				t.Errorf("got %v\nwant %v", gotColumnCount, tt.wantColumnCount)
			}
			if gotRelationCount != tt.wantRelationCount {
				t.Errorf("got %v\nwant %v", gotRelationCount, tt.wantRelationCount)
			}
		})
	}
}

func newTestSchemaViaJSON(t *testing.T) *schema.Schema {
	t.Helper()
	s := &schema.Schema{}
	file, err := os.Open(filepath.Join(testdataDir(), "test_schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(file)
	if err := dec.Decode(s); err != nil {
		t.Fatal(err)
	}
	if err := s.Repair(); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestInferenceConfigParsing(t *testing.T) {
	tests := []struct {
		name           string
		yaml           string
		wantEnabled    bool
		wantEnumMaxCard float64
	}{
		{
			name: "inference as boolean true",
			yaml: `
stats:
  enabled: true
  inference: true
`,
			wantEnabled:    true,
			wantEnumMaxCard: 0.01, // default value after setDefault
		},
		{
			name: "inference as boolean false",
			yaml: `
stats:
  enabled: true
  inference: false
`,
			wantEnabled:    false,
			wantEnumMaxCard: 0, // not set since not enabled
		},
		{
			name: "inference as object",
			yaml: `
stats:
  enabled: true
  inference:
    enabled: true
    enumMaxCardinality: 0.02
`,
			wantEnabled:    true,
			wantEnumMaxCard: 0.02,
		},
		{
			name: "inference as object with custom values",
			yaml: `
stats:
  enabled: true
  inference:
    enabled: true
    enumMaxCardinality: 0.03
    enumMaxDistinct: 30
    dictMaxCardinality: 0.1
    dictMaxDistinct: 200
    foreignKeyMinConfidence: 0.8
`,
			wantEnabled:    true,
			wantEnumMaxCard: 0.03,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := New()
			if err != nil {
				t.Fatal(err)
			}
			if err := c.LoadConfig([]byte(tt.yaml)); err != nil {
				t.Fatal(err)
			}
			if err := c.setDefault(); err != nil {
				t.Fatal(err)
			}

			if c.Stats.Inference.Enabled != tt.wantEnabled {
				t.Errorf("Inference.Enabled = %v, want %v", c.Stats.Inference.Enabled, tt.wantEnabled)
			}
			if c.Stats.Inference.EnumMaxCardinality != tt.wantEnumMaxCard {
				t.Errorf("Inference.EnumMaxCardinality = %v, want %v", c.Stats.Inference.EnumMaxCardinality, tt.wantEnumMaxCard)
			}
		})
	}
}

func TestInferenceConfigDefaults(t *testing.T) {
	yaml := `
stats:
  enabled: true
  inference: true
`
	c, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if err := c.LoadConfig([]byte(yaml)); err != nil {
		t.Fatal(err)
	}
	if err := c.setDefault(); err != nil {
		t.Fatal(err)
	}

	defaults := DefaultInferenceConfig()

	if c.Stats.Inference.EnumMaxCardinality != defaults.EnumMaxCardinality {
		t.Errorf("EnumMaxCardinality = %v, want %v", c.Stats.Inference.EnumMaxCardinality, defaults.EnumMaxCardinality)
	}
	if c.Stats.Inference.EnumMaxDistinct != defaults.EnumMaxDistinct {
		t.Errorf("EnumMaxDistinct = %v, want %v", c.Stats.Inference.EnumMaxDistinct, defaults.EnumMaxDistinct)
	}
	if c.Stats.Inference.DictMaxCardinality != defaults.DictMaxCardinality {
		t.Errorf("DictMaxCardinality = %v, want %v", c.Stats.Inference.DictMaxCardinality, defaults.DictMaxCardinality)
	}
	if c.Stats.Inference.DictMaxDistinct != defaults.DictMaxDistinct {
		t.Errorf("DictMaxDistinct = %v, want %v", c.Stats.Inference.DictMaxDistinct, defaults.DictMaxDistinct)
	}
	if c.Stats.Inference.ForeignKeyMinConfidence != defaults.ForeignKeyMinConfidence {
		t.Errorf("ForeignKeyMinConfidence = %v, want %v", c.Stats.Inference.ForeignKeyMinConfidence, defaults.ForeignKeyMinConfidence)
	}
}
