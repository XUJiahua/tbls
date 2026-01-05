package cmd

import (
	"testing"

	"github.com/k1LoW/tbls/config"
	"github.com/k1LoW/tbls/schema"
)

func TestGenerateScaffoldConfig(t *testing.T) {
	// Create a minimal config
	c := &config.Config{
		DSN: config.DSN{
			URL: "postgres://user:pass@localhost:5432/testdb",
		},
		DocPath: "dbdoc",
		ER: config.ER{
			Format: "svg",
		},
	}
	c.ER.Distance = &config.DefaultERDistance

	// Create a minimal schema
	s := &schema.Schema{
		Name: "testdb",
		Tables: []*schema.Table{
			{
				Name:    "users",
				Comment: "Users table",
				Columns: []*schema.Column{
					{Name: "id", Comment: "Primary key"},
					{Name: "name", Comment: ""},
					{Name: "email", Comment: "User email"},
				},
				Indexes: []*schema.Index{
					{Name: "users_pkey", Comment: ""},
				},
			},
			{
				Name:    "posts",
				Comment: "",
				Columns: []*schema.Column{
					{Name: "id", Comment: ""},
					{Name: "user_id", Comment: ""}, // Should infer relation to users.id
					{Name: "title", Comment: "Post title"},
				},
			},
		},
	}

	// Add users table reference for relation inference
	usersTable := s.Tables[0]
	usersTable.Columns[0].PK = true

	scaffolded, err := GenerateScaffoldConfig(c, s)
	if err != nil {
		t.Fatalf("generateScaffoldConfig failed: %v", err)
	}

	// Verify DSN is preserved
	if scaffolded.DSN.URL != c.DSN.URL {
		t.Errorf("DSN.URL mismatch: got %s, want %s", scaffolded.DSN.URL, c.DSN.URL)
	}

	// Verify DocPath is preserved
	if scaffolded.DocPath != c.DocPath {
		t.Errorf("DocPath mismatch: got %s, want %s", scaffolded.DocPath, c.DocPath)
	}

	// Verify ER settings
	if scaffolded.ER.Format != "svg" {
		t.Errorf("ER.Format mismatch: got %s, want svg", scaffolded.ER.Format)
	}
	if scaffolded.ER.Distance != 1 {
		t.Errorf("ER.Distance mismatch: got %d, want 1", scaffolded.ER.Distance)
	}

	// Verify comments were generated for all tables
	if len(scaffolded.Comments) != 2 {
		t.Errorf("Comments count mismatch: got %d, want 2", len(scaffolded.Comments))
	}

	// Verify first table comments
	usersComment := scaffolded.Comments[0]
	if usersComment.Table != "users" {
		t.Errorf("First table name mismatch: got %s, want users", usersComment.Table)
	}
	if usersComment.TableComment != "Users table" {
		t.Errorf("Table comment mismatch: got %s, want 'Users table'", usersComment.TableComment)
	}
	if len(usersComment.ColumnComments) != 3 {
		t.Errorf("Column comments count mismatch: got %d, want 3", len(usersComment.ColumnComments))
	}
	if usersComment.ColumnComments["id"] != "Primary key" {
		t.Errorf("Column comment for 'id' mismatch: got %s, want 'Primary key'", usersComment.ColumnComments["id"])
	}
	if usersComment.ColumnComments["name"] != "" {
		t.Errorf("Column comment for 'name' should be empty, got %s", usersComment.ColumnComments["name"])
	}

	// Verify inferred relations
	foundUserIdRelation := false
	for _, r := range scaffolded.Relations {
		if r.Table == "posts" && len(r.Columns) == 1 && r.Columns[0] == "user_id" &&
			r.ParentTable == "users" && len(r.ParentColumns) == 1 && r.ParentColumns[0] == "id" {
			foundUserIdRelation = true
			if r.Def != "Inferred Relation" {
				t.Errorf("Inferred relation Def mismatch: got %s, want 'Inferred Relation'", r.Def)
			}
			break
		}
	}
	if !foundUserIdRelation {
		t.Error("Expected inferred relation posts.user_id -> users.id not found")
	}
}

func TestBuildCommentsFromSchema(t *testing.T) {
	s := &schema.Schema{
		Tables: []*schema.Table{
			{
				Name:    "users",
				Comment: "DB Comment",
				Columns: []*schema.Column{
					{Name: "id", Comment: "ID Column"},
					{Name: "name", Comment: ""},
				},
			},
		},
	}

	// Test without existing comments
	comments := buildCommentsFromSchema(s, nil)
	if len(comments) != 1 {
		t.Fatalf("Expected 1 comment, got %d", len(comments))
	}
	if comments[0].TableComment != "DB Comment" {
		t.Errorf("TableComment mismatch: got %s, want 'DB Comment'", comments[0].TableComment)
	}
	if comments[0].ColumnComments["id"] != "ID Column" {
		t.Errorf("Column id comment mismatch: got %s, want 'ID Column'", comments[0].ColumnComments["id"])
	}
	if comments[0].ColumnComments["name"] != "" {
		t.Errorf("Column name comment should be empty, got %s", comments[0].ColumnComments["name"])
	}

	// Test with existing comments that override
	existingComments := []config.AdditionalComment{
		{
			Table:        "users",
			TableComment: "Override Comment",
			ColumnComments: map[string]string{
				"name": "Override Name",
			},
		},
	}
	comments = buildCommentsFromSchema(s, existingComments)
	if comments[0].TableComment != "Override Comment" {
		t.Errorf("TableComment should be overridden: got %s, want 'Override Comment'", comments[0].TableComment)
	}
	if comments[0].ColumnComments["name"] != "Override Name" {
		t.Errorf("Column name comment should be overridden: got %s, want 'Override Name'", comments[0].ColumnComments["name"])
	}
	// DB comment should still be used for non-overridden columns
	if comments[0].ColumnComments["id"] != "ID Column" {
		t.Errorf("Column id comment should come from DB: got %s, want 'ID Column'", comments[0].ColumnComments["id"])
	}
}

func TestRelationKey(t *testing.T) {
	key1 := relationKey("posts", []string{"user_id"}, "users", []string{"id"})
	key2 := relationKey("posts", []string{"user_id"}, "users", []string{"id"})
	key3 := relationKey("comments", []string{"post_id"}, "posts", []string{"id"})

	if key1 != key2 {
		t.Errorf("Same relation should produce same key: got %s and %s", key1, key2)
	}
	if key1 == key3 {
		t.Errorf("Different relations should produce different keys: got %s and %s", key1, key3)
	}
}
