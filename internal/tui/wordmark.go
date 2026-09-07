package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// The launcher's wordmark: MISSION over CONTROL in block glyphs, with the two
// initials in purple. A block font rather than a real one, because the start
// screen is the one place the tool gets to look like something and five rows of
// block glyphs render the same in every terminal.
//
// Two lines rather than one: MISSIONCONTROL on a single line is fourteen
// letters wide, which no ordinary terminal fits beside anything else.
var glyphs = map[rune][5]string{
	'M': {"█   █", "██ ██", "█ █ █", "█   █", "█   █"},
	'I': {"█████", "  █  ", "  █  ", "  █  ", "█████"},
	'S': {"█████", "█    ", "█████", "    █", "█████"},
	'O': {"█████", "█   █", "█   █", "█   █", "█████"},
	'N': {"█   █", "██  █", "█ █ █", "█  ██", "█   █"},
	'C': {"█████", "█    ", "█    ", "█    ", "█████"},
	'T': {"█████", "  █  ", "  █  ", "  █  ", "  █  "},
	'R': {"████ ", "█   █", "████ ", "█  █ ", "█   █"},
	'L': {"█    ", "█    ", "█    ", "█    ", "█████"},
}

// wordmarkWidth is the column count of one word: seven glyphs of five columns
// with a column between them.
const wordmarkWidth = 7*5 + 6

// styWordmark is the wordmark's ordinary weight; styInitial is the purple the
// M and the C wear, the same purple as the logo's lenses.
var (
	styWordmark = lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Bold(true)
	styInitial  = lipgloss.NewStyle().Foreground(colLens).Bold(true)
)

// wordmark renders MISSION over CONTROL, colouring only the first letter of
// each word.
func wordmark() string {
	return word("MISSION") + "\n\n" + word("CONTROL")
}

// word renders one word, the first glyph in purple and the rest plain. Each
// glyph is styled on its own rather than the row being sliced by column, so
// changing a letter's width cannot silently shift the colouring.
func word(s string) string {
	rows := make([]string, 5)
	for i, r := range s {
		g, ok := glyphs[r]
		if !ok {
			continue
		}
		sty := styWordmark
		if i == 0 {
			sty = styInitial
		}
		for y := 0; y < 5; y++ {
			if i > 0 {
				rows[y] += " "
			}
			rows[y] += sty.Render(g[y])
		}
	}
	return strings.Join(rows, "\n")
}
