package tui

import (
	"regexp"
	"strings"

	"charm.land/lipgloss/v2"
)

// A small markdown renderer, enough for what Claude writes into a reply:
// headings, bold/inline-code emphasis, bullet and numbered lists, fenced code
// blocks, and GitHub tables. It is deliberately not a full CommonMark engine —
// that was a 30-dependency import for a single pane. Anything it does not
// understand falls through as its own text, which is the safe default.

var (
	styMdHead = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	styMdBold = lipgloss.NewStyle().Bold(true)
	styMdCode = lipgloss.NewStyle().Foreground(colOpen)
	styMdRule = lipgloss.NewStyle().Foreground(colFaint)

	inlineRE = regexp.MustCompile("`[^`]+`|\\*\\*[^*]+\\*\\*|__[^_]+__")
)

// renderMarkdown turns src into styled terminal lines wrapped to width.
func renderMarkdown(src string, width int) []string {
	if width < 8 {
		return wrap(src, max(width, 1))
	}
	var out []string
	lines := strings.Split(src, "\n")
	for i := 0; i < len(lines); i++ {
		line := lines[i]

		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			for i++; i < len(lines); i++ {
				if strings.HasPrefix(strings.TrimSpace(lines[i]), "```") {
					break
				}
				out = append(out, styMdCode.Render(truncate("  "+lines[i], width)))
			}
			continue
		}

		if isTableRow(line) && i+1 < len(lines) && isTableSeparator(lines[i+1]) {
			var rows []string
			for i < len(lines) && isTableRow(lines[i]) {
				rows = append(rows, lines[i])
				i++
			}
			i--
			out = append(out, renderTable(rows, width)...)
			continue
		}

		out = append(out, renderBlock(line, width)...)
	}
	for len(out) > 0 && out[0] == "" {
		out = out[1:]
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return out
}

// renderBlock handles one non-code, non-table line: heading, rule, list item or
// paragraph, wrapping the text to width and styling inline spans.
func renderBlock(line string, width int) []string {
	trimmed := strings.TrimSpace(line)
	switch {
	case trimmed == "":
		return []string{""}

	case trimmed == "---" || trimmed == "***" || trimmed == "___":
		return []string{styMdRule.Render(strings.Repeat("─", width))}

	case strings.HasPrefix(trimmed, "#"):
		text := strings.TrimLeft(trimmed, "# ")
		var lines []string
		for _, w := range wrap(text, width) {
			lines = append(lines, styMdHead.Render(w))
		}
		return lines
	}

	// A list item keeps its marker on the first line and hangs the wrap under it.
	indent, marker, rest := listItem(line)
	if marker != "" {
		hang := indent + strings.Repeat(" ", len(marker)+1)
		wrapped := wrap(rest, max(width-lipgloss.Width(hang), 4))
		var lines []string
		for j, w := range wrapped {
			lead := indent + marker + " "
			if j > 0 {
				lead = hang
			}
			lines = append(lines, lead+styleInline(w))
		}
		return lines
	}

	var lines []string
	for _, w := range wrap(line, width) {
		lines = append(lines, styleInline(w))
	}
	return lines
}

var listRE = regexp.MustCompile(`^(\s*)([-*+]|\d{1,2}[.)])\s+(.*)`)

func listItem(line string) (indent, marker, rest string) {
	m := listRE.FindStringSubmatch(line)
	if m == nil {
		return "", "", ""
	}
	mk := m[2]
	if mk == "*" || mk == "+" {
		mk = "-"
	}
	return m[1], mk, m[3]
}

// styleInline styles `code` and **bold** spans. It runs after wrapping, so a
// span the wrap split across lines simply renders unstyled — acceptable, and it
// keeps width accounting honest since the styled bytes never reach wrap.
func styleInline(s string) string {
	return inlineRE.ReplaceAllStringFunc(s, func(tok string) string {
		switch {
		case strings.HasPrefix(tok, "`"):
			return styMdCode.Render(strings.Trim(tok, "`"))
		case strings.HasPrefix(tok, "**"):
			return styMdBold.Render(strings.Trim(tok, "*"))
		default:
			return styMdBold.Render(strings.Trim(tok, "_"))
		}
	})
}

// isTableRow accepts GitHub's loose form: the outer pipes are optional, so a
// row may open with a cell — "[L1] | …" is how the operating rules tag rows.
func isTableRow(line string) bool {
	return strings.Contains(line, "|")
}

var tableSepRE = regexp.MustCompile(`^\s*\|?[\s:|-]*-[\s:|-]*\|?\s*$`)

func isTableSeparator(line string) bool {
	return isTableRow(line) && tableSepRE.MatchString(line) && strings.Contains(line, "-")
}

func tableCells(line string) []string {
	t := strings.TrimSpace(line)
	t = strings.TrimPrefix(t, "|")
	t = strings.TrimSuffix(t, "|")
	cells := strings.Split(t, "|")
	for i := range cells {
		cells[i] = strings.TrimSpace(cells[i])
	}
	return cells
}

// renderTable draws a header row, a rule, then the body, sizing columns to their
// content but never past the pane. The header row is styled; the separator row
// in the source is dropped by the caller's row collection.
func renderTable(rows []string, width int) []string {
	if len(rows) < 2 {
		return rows
	}
	header := tableCells(rows[0])
	body := make([][]string, 0, len(rows)-2)
	for _, r := range rows[2:] {
		body = append(body, tableCells(r))
	}

	cols := len(header)
	widths := make([]int, cols)
	for c := 0; c < cols; c++ {
		widths[c] = lipgloss.Width(header[c])
	}
	for _, row := range body {
		for c := 0; c < cols && c < len(row); c++ {
			if w := lipgloss.Width(row[c]); w > widths[c] {
				widths[c] = w
			}
		}
	}
	// A table wider than the pane gives up room from its widest column first,
	// one cell at a time: a four-character tag column stays whole while the
	// prose column, which can wrap, takes the squeeze.
	gutters := 3 * (cols - 1)
	for sum := func() int {
		n := gutters
		for _, w := range widths {
			n += w
		}
		return n
	}; sum() > width; {
		widest := 0
		for c := range widths {
			if widths[c] > widths[widest] {
				widest = c
			}
		}
		if widths[widest] <= 3 {
			break
		}
		widths[widest]--
	}

	// A cell wider than its column continues on the next line of the same row
	// rather than being cut: what Claude wrote is the point of the table.
	line := func(cells []string, style lipgloss.Style) []string {
		wrapped := make([][]string, cols)
		depth := 1
		for c := 0; c < cols; c++ {
			cell := ""
			if c < len(cells) {
				cell = cells[c]
			}
			wrapped[c] = wrap(cell, widths[c])
			depth = max(depth, len(wrapped[c]))
		}
		out := make([]string, depth)
		for i := range out {
			parts := make([]string, cols)
			for c := 0; c < cols; c++ {
				cell := ""
				if i < len(wrapped[c]) {
					cell = wrapped[c][i]
				}
				parts[c] = style.Render(pad(styleInline(truncate(cell, widths[c])), widths[c]))
			}
			out[i] = strings.Join(parts, styMdRule.Render(" │ "))
		}
		return out
	}

	out := line(header, styMdBold)
	seps := make([]string, cols)
	for c := range seps {
		seps[c] = strings.Repeat("─", widths[c])
	}
	out = append(out, styMdRule.Render(strings.Join(seps, "─┼─")))
	for _, row := range body {
		out = append(out, line(row, lipgloss.NewStyle())...)
	}
	return out
}

func pad(s string, w int) string {
	if d := w - lipgloss.Width(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}
