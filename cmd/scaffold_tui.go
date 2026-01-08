package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/k1LoW/tbls/schema"
	"github.com/sahilm/fuzzy"
)

// Styles
var (
	selectedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))  // Green
	cursorStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("14"))  // Cyan
	dimStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("240")) // Gray
	countStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("245")) // Light gray
	headerStyle     = lipgloss.NewStyle().Bold(true)
	statusBarStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
)

// tableItem represents a selectable table
type tableItem struct {
	name     string
	colCount int
	selected bool
}

// String returns the display string for fuzzy matching
func (t tableItem) String() string {
	return t.name
}

// tableSelectorModel is the bubbletea model for table selection
type tableSelectorModel struct {
	tables      []tableItem
	filtered    []int // indices into tables
	cursor      int   // cursor position in filtered list
	filter      textinput.Model
	quitting    bool
	cancelled   bool
	windowWidth int
	windowHeight int
}

// newTableSelectorModel creates a new table selector model
func newTableSelectorModel(tables []*schema.Table) tableSelectorModel {
	items := make([]tableItem, len(tables))
	for i, t := range tables {
		items[i] = tableItem{
			name:     t.Name,
			colCount: len(t.Columns),
			selected: false,
		}
	}

	// Initialize all indices as filtered (no filter applied)
	filtered := make([]int, len(items))
	for i := range items {
		filtered[i] = i
	}

	ti := textinput.New()
	ti.Placeholder = "Type to filter..."
	ti.Focus()
	ti.CharLimit = 100
	ti.Width = 40

	return tableSelectorModel{
		tables:   items,
		filtered: filtered,
		cursor:   0,
		filter:   ti,
	}
}

// Init initializes the model
func (m tableSelectorModel) Init() tea.Cmd {
	return textinput.Blink
}

// Update handles messages
func (m tableSelectorModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.windowWidth = msg.Width
		m.windowHeight = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			m.cancelled = true
			m.quitting = true
			return m, tea.Quit

		case "enter":
			m.quitting = true
			return m, tea.Quit

		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
			return m, nil

		case "down", "j":
			if m.cursor < len(m.filtered)-1 {
				m.cursor++
			}
			return m, nil

		case "tab", " ":
			// Toggle selection
			if len(m.filtered) > 0 && m.cursor < len(m.filtered) {
				idx := m.filtered[m.cursor]
				m.tables[idx].selected = !m.tables[idx].selected
			}
			return m, nil

		case "ctrl+a":
			// Select all filtered
			for _, idx := range m.filtered {
				m.tables[idx].selected = true
			}
			return m, nil

		case "ctrl+d":
			// Deselect all
			for i := range m.tables {
				m.tables[i].selected = false
			}
			return m, nil
		}
	}

	// Handle text input for filter
	prevFilter := m.filter.Value()
	m.filter, cmd = m.filter.Update(msg)

	// If filter changed, update filtered list
	if m.filter.Value() != prevFilter {
		m.updateFilter()
	}

	return m, cmd
}

// updateFilter updates the filtered list based on current filter
func (m *tableSelectorModel) updateFilter() {
	query := m.filter.Value()
	if query == "" {
		// No filter, show all
		m.filtered = make([]int, len(m.tables))
		for i := range m.tables {
			m.filtered[i] = i
		}
	} else {
		// Fuzzy match
		tableStrings := make([]string, len(m.tables))
		for i, t := range m.tables {
			tableStrings[i] = t.name
		}
		matches := fuzzy.Find(query, tableStrings)
		m.filtered = make([]int, len(matches))
		for i, match := range matches {
			m.filtered[i] = match.Index
		}
	}

	// Reset cursor if out of bounds
	if m.cursor >= len(m.filtered) {
		m.cursor = max(0, len(m.filtered)-1)
	}
}

// View renders the UI
func (m tableSelectorModel) View() string {
	if m.quitting {
		return ""
	}

	var b strings.Builder

	// Header
	total := len(m.tables)
	selected := m.countSelected()
	header := fmt.Sprintf("Found %d tables. ↑/↓ navigate, Tab select, Ctrl+A all, Enter confirm, Esc cancel", total)
	b.WriteString(headerStyle.Render(header))
	b.WriteString("\n\n")

	// Filter input
	b.WriteString("Filter: ")
	b.WriteString(m.filter.View())
	b.WriteString("\n\n")

	// Calculate visible lines
	maxVisible := 15
	if m.windowHeight > 0 {
		maxVisible = m.windowHeight - 8 // Reserve space for header, filter, status
		if maxVisible < 5 {
			maxVisible = 5
		}
	}

	// Calculate scroll offset
	start := 0
	if m.cursor >= maxVisible {
		start = m.cursor - maxVisible + 1
	}
	end := start + maxVisible
	if end > len(m.filtered) {
		end = len(m.filtered)
	}

	// Render table list
	for i := start; i < end; i++ {
		idx := m.filtered[i]
		item := m.tables[idx]

		// Cursor
		cursor := "  "
		if i == m.cursor {
			cursor = cursorStyle.Render("> ")
		}

		// Selection checkbox
		checkbox := "[ ]"
		if item.selected {
			checkbox = selectedStyle.Render("[✓]")
		}

		// Table name and column count
		name := item.name
		if i == m.cursor {
			name = cursorStyle.Render(name)
		}
		colInfo := countStyle.Render(fmt.Sprintf(" (%d cols)", item.colCount))

		b.WriteString(fmt.Sprintf("%s%s %s%s\n", cursor, checkbox, name, colInfo))
	}

	// Show scroll indicator if needed
	if len(m.filtered) > maxVisible {
		scrollInfo := dimStyle.Render(fmt.Sprintf("\n  ... showing %d-%d of %d", start+1, end, len(m.filtered)))
		b.WriteString(scrollInfo)
		b.WriteString("\n")
	}

	// Status bar
	b.WriteString("\n")
	status := statusBarStyle.Render(fmt.Sprintf("Selected: %d/%d", selected, total))
	b.WriteString(status)

	return b.String()
}

// countSelected returns the count of selected tables
func (m tableSelectorModel) countSelected() int {
	count := 0
	for _, t := range m.tables {
		if t.selected {
			count++
		}
	}
	return count
}

// getSelectedTables returns the names of selected tables
func (m tableSelectorModel) getSelectedTables() []string {
	var selected []string
	for _, t := range m.tables {
		if t.selected {
			selected = append(selected, t.name)
		}
	}
	return selected
}

// RunTableSelector runs the interactive table selector and returns selected table names
func RunTableSelector(tables []*schema.Table) ([]string, error) {
	if len(tables) == 0 {
		return nil, fmt.Errorf("no tables found in database")
	}

	// Check if running in a terminal
	if !isTerminal() {
		return nil, fmt.Errorf("interactive mode requires a terminal")
	}

	m := newTableSelectorModel(tables)
	p := tea.NewProgram(m, tea.WithAltScreen())

	finalModel, err := p.Run()
	if err != nil {
		return nil, fmt.Errorf("failed to run table selector: %w", err)
	}

	final := finalModel.(tableSelectorModel)
	if final.cancelled {
		return nil, fmt.Errorf("cancelled")
	}

	return final.getSelectedTables(), nil
}

// isTerminal checks if stdout is a terminal
func isTerminal() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// filterSchemaToTables filters a schema to only include the specified tables
func filterSchemaToTables(s *schema.Schema, tableNames []string) *schema.Schema {
	nameSet := make(map[string]bool)
	for _, name := range tableNames {
		nameSet[name] = true
	}

	var filteredTables []*schema.Table
	for _, t := range s.Tables {
		if nameSet[t.Name] {
			filteredTables = append(filteredTables, t)
		}
	}

	// Create a new schema with filtered tables
	filtered := *s
	filtered.Tables = filteredTables

	// Filter relations to only include those between selected tables
	var filteredRelations []*schema.Relation
	for _, r := range s.Relations {
		if nameSet[r.Table.Name] && nameSet[r.ParentTable.Name] {
			filteredRelations = append(filteredRelations, r)
		}
	}
	filtered.Relations = filteredRelations

	return &filtered
}
