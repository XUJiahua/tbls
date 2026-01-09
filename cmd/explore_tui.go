package cmd

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/k1LoW/tbls/schema"
	"github.com/mattn/go-runewidth"
	"github.com/sahilm/fuzzy"
)

// Node types for the tree
type nodeType int

const (
	nodeDatabase nodeType = iota
	nodeTable
	nodeColumn
)

// treeNode represents a node in the tree
type treeNode struct {
	nodeType  nodeType
	name      string
	expanded  bool
	table     *schema.Table
	column    *schema.Column
	children  []*treeNode
	parent    *treeNode
	depth     int
	lastChild bool // for rendering tree lines
}

// explorerModel is the main TUI model
type explorerModel struct {
	schema       *schema.Schema
	root         *treeNode
	flatNodes    []*treeNode // flattened visible nodes for navigation
	cursor       int
	filter       textinput.Model
	searching    bool
	searchQuery  string
	matchIndices []int // indices of matching nodes in flatNodes
	matchCursor  int   // current position in matchIndices
	quitting     bool
	windowWidth  int
	windowHeight int
}

// Styles
var (
	explorerTitleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	explorerSelectedStyle = lipgloss.NewStyle().Background(lipgloss.Color("236")).Bold(true)
	explorerDimStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	explorerTreeStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
	explorerTableStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
	explorerColumnStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("15"))
	explorerTypeStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	explorerStatLabel     = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	explorerStatValue     = lipgloss.NewStyle().Foreground(lipgloss.Color("15"))
	explorerGoodStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	explorerWarnStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	explorerBadStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	explorerBarStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("12"))
	explorerMatchStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true)
	explorerBorderStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
	explorerHelpStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
)

func newExplorerModel(s *schema.Schema) explorerModel {
	// Build tree structure
	root := &treeNode{
		nodeType: nodeDatabase,
		name:     s.Name,
		expanded: true,
		depth:    0,
	}

	for i, t := range s.Tables {
		tableNode := &treeNode{
			nodeType:  nodeTable,
			name:      t.Name,
			table:     t,
			parent:    root,
			depth:     1,
			lastChild: i == len(s.Tables)-1,
		}

		for j, c := range t.Columns {
			colNode := &treeNode{
				nodeType:  nodeColumn,
				name:      c.Name,
				column:    c,
				table:     t,
				parent:    tableNode,
				depth:     2,
				lastChild: j == len(t.Columns)-1,
			}
			tableNode.children = append(tableNode.children, colNode)
		}

		root.children = append(root.children, tableNode)
	}

	ti := textinput.New()
	ti.Placeholder = "Search tables and columns..."
	ti.CharLimit = 100
	ti.Width = 40

	m := explorerModel{
		schema: s,
		root:   root,
		filter: ti,
	}
	m.updateFlatNodes()

	return m
}

// updateFlatNodes rebuilds the flat list of visible nodes
func (m *explorerModel) updateFlatNodes() {
	m.flatNodes = nil
	m.flattenNode(m.root)
	if m.cursor >= len(m.flatNodes) {
		m.cursor = max(0, len(m.flatNodes)-1)
	}
}

func (m *explorerModel) flattenNode(n *treeNode) {
	m.flatNodes = append(m.flatNodes, n)
	if n.expanded {
		for _, child := range n.children {
			m.flattenNode(child)
		}
	}
}

func (m explorerModel) Init() tea.Cmd {
	return nil
}

func (m explorerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.windowWidth = msg.Width
		m.windowHeight = msg.Height
		return m, nil

	case tea.KeyMsg:
		if m.searching {
			return m.handleSearchInput(msg)
		}
		return m.handleNavigation(msg)
	}

	return m, cmd
}

func (m explorerModel) handleSearchInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.searching = false
		m.searchQuery = ""
		m.filter.SetValue("")
		m.matchIndices = nil
		return m, nil

	case "enter":
		m.searching = false
		// Jump to first match if any
		if len(m.matchIndices) > 0 {
			m.cursor = m.matchIndices[m.matchCursor]
		}
		return m, nil

	case "ctrl+n", "down":
		// Next match
		if len(m.matchIndices) > 0 {
			m.matchCursor = (m.matchCursor + 1) % len(m.matchIndices)
			m.cursor = m.matchIndices[m.matchCursor]
		}
		return m, nil

	case "ctrl+p", "up":
		// Previous match
		if len(m.matchIndices) > 0 {
			m.matchCursor--
			if m.matchCursor < 0 {
				m.matchCursor = len(m.matchIndices) - 1
			}
			m.cursor = m.matchIndices[m.matchCursor]
		}
		return m, nil
	}

	// Handle text input
	prevFilter := m.filter.Value()
	var cmd tea.Cmd
	m.filter, cmd = m.filter.Update(msg)

	if m.filter.Value() != prevFilter {
		m.searchQuery = m.filter.Value()
		m.updateSearchMatches()
	}

	return m, cmd
}

func (m *explorerModel) updateSearchMatches() {
	m.matchIndices = nil
	m.matchCursor = 0

	if m.searchQuery == "" {
		return
	}

	// Collect all searchable strings
	var searchStrings []string
	for _, n := range m.flatNodes {
		searchStrings = append(searchStrings, n.name)
	}

	// Fuzzy match
	matches := fuzzy.Find(m.searchQuery, searchStrings)
	for _, match := range matches {
		m.matchIndices = append(m.matchIndices, match.Index)
	}

	// Move cursor to first match
	if len(m.matchIndices) > 0 {
		m.cursor = m.matchIndices[0]
	}
}

func (m explorerModel) handleNavigation(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		m.quitting = true
		return m, tea.Quit

	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
		return m, nil

	case "down", "j":
		if m.cursor < len(m.flatNodes)-1 {
			m.cursor++
		}
		return m, nil

	case "enter", "right", "l":
		if m.cursor < len(m.flatNodes) {
			node := m.flatNodes[m.cursor]
			if len(node.children) > 0 {
				node.expanded = !node.expanded
				m.updateFlatNodes()
			}
		}
		return m, nil

	case "left", "h":
		if m.cursor < len(m.flatNodes) {
			node := m.flatNodes[m.cursor]
			if node.expanded && len(node.children) > 0 {
				// Collapse current node
				node.expanded = false
				m.updateFlatNodes()
			} else if node.parent != nil {
				// Go to parent
				for i, n := range m.flatNodes {
					if n == node.parent {
						m.cursor = i
						break
					}
				}
			}
		}
		return m, nil

	case "/":
		m.searching = true
		m.filter.Focus()
		return m, textinput.Blink

	case "n":
		// Next match
		if len(m.matchIndices) > 0 {
			m.matchCursor = (m.matchCursor + 1) % len(m.matchIndices)
			m.cursor = m.matchIndices[m.matchCursor]
		}
		return m, nil

	case "N":
		// Previous match
		if len(m.matchIndices) > 0 {
			m.matchCursor--
			if m.matchCursor < 0 {
				m.matchCursor = len(m.matchIndices) - 1
			}
			m.cursor = m.matchIndices[m.matchCursor]
		}
		return m, nil
	}

	return m, nil
}

func (m explorerModel) View() string {
	if m.quitting {
		return ""
	}

	// Calculate layout
	leftWidth := 40
	if m.windowWidth > 120 {
		leftWidth = 50
	}
	rightWidth := m.windowWidth - leftWidth - 3 // 3 for border

	// Build left panel (tree)
	leftPanel := m.renderTreePanel(leftWidth)

	// Build right panel (details)
	rightPanel := m.renderDetailPanel(rightWidth)

	// Combine panels
	border := explorerBorderStyle.Render("│")
	var lines []string

	leftLines := strings.Split(leftPanel, "\n")
	rightLines := strings.Split(rightPanel, "\n")

	maxLines := max(len(leftLines), len(rightLines))
	for i := 0; i < maxLines; i++ {
		left := ""
		if i < len(leftLines) {
			left = leftLines[i]
		}
		right := ""
		if i < len(rightLines) {
			right = rightLines[i]
		}
		// Pad left to width
		left = padRight(left, leftWidth)
		lines = append(lines, left+border+right)
	}

	content := strings.Join(lines, "\n")

	// Add help bar
	helpText := m.renderHelpBar()
	content += "\n" + explorerBorderStyle.Render(strings.Repeat("─", m.windowWidth)) + "\n"
	content += helpText

	return content
}

func (m explorerModel) renderTreePanel(width int) string {
	var b strings.Builder

	// Header
	title := "Tables"
	if m.searching {
		title = "Search: " + m.filter.View()
	} else if m.searchQuery != "" {
		title = fmt.Sprintf("Tables (matches: %d)", len(m.matchIndices))
	}
	b.WriteString(explorerTitleStyle.Render(title))
	b.WriteString("\n\n")

	// Calculate visible lines
	maxVisible := m.windowHeight - 6
	if maxVisible < 5 {
		maxVisible = 15
	}

	// Calculate scroll offset
	start := 0
	if m.cursor >= maxVisible {
		start = m.cursor - maxVisible + 1
	}
	end := start + maxVisible
	if end > len(m.flatNodes) {
		end = len(m.flatNodes)
	}

	// Render nodes
	for i := start; i < end; i++ {
		node := m.flatNodes[i]
		line := m.renderTreeNode(node, i == m.cursor, width-4)
		b.WriteString(line)
		b.WriteString("\n")
	}

	// Scroll indicator
	if len(m.flatNodes) > maxVisible {
		scrollInfo := explorerDimStyle.Render(fmt.Sprintf(" %d-%d of %d", start+1, end, len(m.flatNodes)))
		b.WriteString(scrollInfo)
	}

	return b.String()
}

func (m explorerModel) renderTreeNode(node *treeNode, selected bool, maxWidth int) string {
	var prefix string

	// Build tree prefix
	if node.depth > 0 {
		for i := 0; i < node.depth-1; i++ {
			prefix += "  "
		}
		if node.lastChild {
			prefix += "└─"
		} else {
			prefix += "├─"
		}
	}

	// Expand/collapse indicator
	indicator := " "
	if len(node.children) > 0 {
		if node.expanded {
			indicator = "▼"
		} else {
			indicator = "►"
		}
	}

	// Node name with styling
	name := node.name
	switch node.nodeType {
	case nodeDatabase:
		name = explorerTitleStyle.Render(name)
	case nodeTable:
		colCount := len(node.table.Columns)
		name = explorerTableStyle.Render(name) + explorerDimStyle.Render(fmt.Sprintf(" (%d)", colCount))
	case nodeColumn:
		typeName := node.column.Type
		if len(typeName) > 15 {
			typeName = typeName[:15] + "…"
		}
		name = explorerColumnStyle.Render(name) + " " + explorerTypeStyle.Render(typeName)
	}

	// Check if this node matches search
	isMatch := false
	for _, idx := range m.matchIndices {
		if idx == m.findNodeIndex(node) {
			isMatch = true
			break
		}
	}
	if isMatch {
		name = explorerMatchStyle.Render("● ") + name
	}

	line := explorerTreeStyle.Render(prefix) + indicator + " " + name

	// Apply selection style
	if selected {
		// Need to strip styles and reapply selection
		line = explorerSelectedStyle.Render(stripAnsi(line))
	}

	return line
}

func (m explorerModel) findNodeIndex(target *treeNode) int {
	for i, n := range m.flatNodes {
		if n == target {
			return i
		}
	}
	return -1
}

func (m explorerModel) renderDetailPanel(width int) string {
	var b strings.Builder

	if m.cursor >= len(m.flatNodes) {
		return ""
	}

	node := m.flatNodes[m.cursor]

	switch node.nodeType {
	case nodeDatabase:
		b.WriteString(m.renderDatabaseDetail(width))
	case nodeTable:
		b.WriteString(m.renderTableDetail(node.table, width))
	case nodeColumn:
		b.WriteString(m.renderColumnDetail(node.column, node.table, width))
	}

	return b.String()
}

func (m explorerModel) renderDatabaseDetail(width int) string {
	var b strings.Builder

	b.WriteString(explorerTitleStyle.Render("Database: "+m.schema.Name) + "\n\n")

	// Summary stats
	totalTables := len(m.schema.Tables)
	totalColumns := 0
	for _, t := range m.schema.Tables {
		totalColumns += len(t.Columns)
	}

	b.WriteString(explorerStatLabel.Render("Tables: ") + explorerStatValue.Render(fmt.Sprintf("%d", totalTables)) + "\n")
	b.WriteString(explorerStatLabel.Render("Total Columns: ") + explorerStatValue.Render(fmt.Sprintf("%d", totalColumns)) + "\n")

	if m.schema.Driver != nil {
		b.WriteString(explorerStatLabel.Render("Driver: ") + explorerStatValue.Render(m.schema.Driver.Name) + "\n")
	}

	return b.String()
}

func (m explorerModel) renderTableDetail(t *schema.Table, width int) string {
	var b strings.Builder

	b.WriteString(explorerTitleStyle.Render("Table: "+t.Name) + "\n")
	b.WriteString(explorerTypeStyle.Render(t.Type) + "\n\n")

	// Comment
	if t.Comment != "" {
		b.WriteString(explorerDimStyle.Render(t.Comment) + "\n\n")
	}

	// Stats
	b.WriteString(explorerStatLabel.Render("── Statistics ──") + "\n")
	b.WriteString(explorerStatLabel.Render("Columns: ") + explorerStatValue.Render(fmt.Sprintf("%d", len(t.Columns))) + "\n")

	if t.Stats != nil {
		b.WriteString(explorerStatLabel.Render("Rows: ") + explorerStatValue.Render(formatNumber(t.Stats.RowCount)) + "\n")
		if t.Stats.DataBytes > 0 {
			b.WriteString(explorerStatLabel.Render("Data Size: ") + explorerStatValue.Render(formatBytes(t.Stats.DataBytes)) + "\n")
		}
	}

	// Column type distribution
	enumCount := 0
	highCardCount := 0
	dictCount := 0
	for _, c := range t.Columns {
		if c.Inferences != nil {
			switch c.Inferences.EnumType {
			case "enum":
				enumCount++
			case "high_cardinality":
				highCardCount++
			case "dictionary":
				dictCount++
			}
		}
	}

	if enumCount > 0 || highCardCount > 0 || dictCount > 0 {
		b.WriteString("\n" + explorerStatLabel.Render("── Column Types ──") + "\n")
		if enumCount > 0 {
			b.WriteString(explorerGoodStyle.Render(fmt.Sprintf("  enum: %d", enumCount)) + "\n")
		}
		if dictCount > 0 {
			b.WriteString(explorerWarnStyle.Render(fmt.Sprintf("  dictionary: %d", dictCount)) + "\n")
		}
		if highCardCount > 0 {
			b.WriteString(explorerDimStyle.Render(fmt.Sprintf("  high_cardinality: %d", highCardCount)) + "\n")
		}
	}

	return b.String()
}

func (m explorerModel) renderColumnDetail(c *schema.Column, t *schema.Table, width int) string {
	var b strings.Builder

	b.WriteString(explorerTitleStyle.Render("Column: "+c.Name) + "\n")
	b.WriteString(explorerTypeStyle.Render(c.Type) + "\n\n")

	// Comment
	if c.Comment != "" {
		b.WriteString(explorerDimStyle.Render(c.Comment) + "\n\n")
	}

	// Basic info
	nullable := "NOT NULL"
	if c.Nullable {
		nullable = "NULLABLE"
	}
	b.WriteString(explorerStatLabel.Render(nullable) + "\n")
	if c.Default.Valid {
		b.WriteString(explorerStatLabel.Render("Default: ") + explorerDimStyle.Render(c.Default.String) + "\n")
	}

	// Stats
	if c.Stats != nil {
		b.WriteString("\n" + explorerStatLabel.Render("── Data Quality ──") + "\n")
		b.WriteString(explorerStatLabel.Render("Rows: ") + explorerStatValue.Render(formatNumber(c.Stats.RowCount)) + "\n")

		// Null rate with color coding
		nullPercent := c.Stats.NullPercent
		nullStyle := explorerGoodStyle
		if nullPercent > 50 {
			nullStyle = explorerBadStyle
		} else if nullPercent > 10 {
			nullStyle = explorerWarnStyle
		}
		b.WriteString(explorerStatLabel.Render("Nulls: ") +
			nullStyle.Render(fmt.Sprintf("%s (%.2f%%)", formatNumber(c.Stats.NullCount), nullPercent)) + "\n")

		// Cardinality
		b.WriteString(explorerStatLabel.Render("Distinct: ") + explorerStatValue.Render(formatNumber(c.Stats.DistinctCount)) + "\n")

		// Numeric stats
		if c.Stats.Min != nil {
			b.WriteString(explorerStatLabel.Render("Min: ") + explorerStatValue.Render(fmt.Sprintf("%.2f", *c.Stats.Min)) + "\n")
		}
		if c.Stats.Max != nil {
			b.WriteString(explorerStatLabel.Render("Max: ") + explorerStatValue.Render(fmt.Sprintf("%.2f", *c.Stats.Max)) + "\n")
		}
		if c.Stats.Avg != nil {
			b.WriteString(explorerStatLabel.Render("Avg: ") + explorerStatValue.Render(fmt.Sprintf("%.2f", *c.Stats.Avg)) + "\n")
		}

		// Date stats
		if c.Stats.MinDate != nil {
			b.WriteString(explorerStatLabel.Render("Min Date: ") + explorerStatValue.Render(*c.Stats.MinDate) + "\n")
		}
		if c.Stats.MaxDate != nil {
			b.WriteString(explorerStatLabel.Render("Max Date: ") + explorerStatValue.Render(*c.Stats.MaxDate) + "\n")
		}

		// String length stats
		if c.Stats.MinLength != nil {
			b.WriteString(explorerStatLabel.Render("Length: ") +
				explorerStatValue.Render(fmt.Sprintf("%d - %d (avg: %.1f)",
					*c.Stats.MinLength, *c.Stats.MaxLength, *c.Stats.AvgLength)) + "\n")
		}
	}

	// Inferences
	if c.Inferences != nil {
		b.WriteString("\n" + explorerStatLabel.Render("── Inferred Type ──") + "\n")

		enumType := c.Inferences.EnumType
		enumStyle := explorerDimStyle
		switch enumType {
		case "enum":
			enumStyle = explorerGoodStyle
		case "dictionary":
			enumStyle = explorerWarnStyle
		case "high_cardinality":
			enumStyle = explorerDimStyle
		}
		if enumType != "" {
			b.WriteString(explorerStatLabel.Render("Type: ") + enumStyle.Render(enumType) + "\n")
		}

		if c.Inferences.Cardinality > 0 {
			b.WriteString(explorerStatLabel.Render("Cardinality: ") +
				explorerStatValue.Render(fmt.Sprintf("%.6f", c.Inferences.Cardinality)) + "\n")
		}

		// Distribution
		if len(c.Inferences.Distribution) > 0 {
			b.WriteString("\n" + explorerStatLabel.Render("── Distribution ──") + "\n")
			b.WriteString(m.renderDistributionChart(c.Inferences.Distribution, width-4))
		} else if c.Stats != nil && len(c.Stats.TopValues) > 0 {
			// Fallback to top values if no distribution
			b.WriteString("\n" + explorerStatLabel.Render("── Top Values ──") + "\n")
			for i, tv := range c.Stats.TopValues {
				if i >= 10 {
					break
				}
				val := tv.Value
				if len(val) > 30 {
					val = val[:30] + "…"
				}
				b.WriteString(fmt.Sprintf("  %s: %s\n",
					explorerStatValue.Render(val),
					explorerDimStyle.Render(formatNumber(tv.Count))))
			}
		}
	}

	return b.String()
}

func (m explorerModel) renderDistributionChart(dist []schema.DistributionItem, width int) string {
	var b strings.Builder

	maxPercent := 0.0
	for _, item := range dist {
		if item.Percent > maxPercent {
			maxPercent = item.Percent
		}
	}

	barWidth := width - 35
	if barWidth < 10 {
		barWidth = 10
	}

	for i, item := range dist {
		if i >= 10 {
			b.WriteString(explorerDimStyle.Render(fmt.Sprintf("  ... and %d more\n", len(dist)-10)))
			break
		}

		val := item.Value
		if len(val) > 15 {
			val = val[:15] + "…"
		}

		// Calculate bar length
		barLen := int(float64(barWidth) * (item.Percent / maxPercent))
		if barLen < 1 && item.Percent > 0 {
			barLen = 1
		}

		bar := strings.Repeat("█", barLen)
		b.WriteString(fmt.Sprintf("  %-16s %s %5.1f%%\n",
			explorerStatValue.Render(val),
			explorerBarStyle.Render(bar),
			item.Percent))
	}

	return b.String()
}

func (m explorerModel) renderHelpBar() string {
	keys := []string{
		"↑↓/jk:navigate",
		"Enter:expand",
		"/:search",
		"n/N:next/prev match",
		"q:quit",
	}
	return explorerHelpStyle.Render(strings.Join(keys, "  "))
}

// RunExplorer runs the TUI explorer
func RunExplorer(s *schema.Schema) error {
	if !isTerminal() {
		return fmt.Errorf("interactive mode requires a terminal")
	}

	m := newExplorerModel(s)
	p := tea.NewProgram(m, tea.WithAltScreen())

	_, err := p.Run()
	return err
}

// Helper functions

func formatNumber(n int64) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	if n < 1000000 {
		return fmt.Sprintf("%.1fK", float64(n)/1000)
	}
	if n < 1000000000 {
		return fmt.Sprintf("%.1fM", float64(n)/1000000)
	}
	return fmt.Sprintf("%.1fB", float64(n)/1000000000)
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func padRight(s string, width int) string {
	// Calculate visible width (handling ANSI codes)
	visibleWidth := runewidth.StringWidth(stripAnsi(s))
	if visibleWidth >= width {
		return s
	}
	return s + strings.Repeat(" ", width-visibleWidth)
}

func stripAnsi(s string) string {
	var b strings.Builder
	inEscape := false
	for _, r := range s {
		if r == '\x1b' {
			inEscape = true
			continue
		}
		if inEscape {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEscape = false
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

