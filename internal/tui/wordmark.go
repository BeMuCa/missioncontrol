package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// The launcher's wordmark: MISSIONCONTROL on one line in block glyphs, with the
// M and the C in purple. A block font rather than a real one, because the start
// screen is the one place the tool gets to look like something and five rows of
// block glyphs render the same in every terminal.
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

// wordmarkWidth is the column count of the whole mark: fourteen glyphs of five
// columns with a column between them. A terminal narrower than this gets the
// plain name instead — see the launcher's render.
const wordmarkWidth = 14*5 + 13

// wordmarkRows is how many text rows the block glyphs occupy.
const wordmarkRows = 5

// word is the text the mark spells, and initials are the two letters that wear
// the purple: the M it starts with and the C that opens CONTROL.
const word = "MISSIONCONTROL"

// initials indexes into word: the M it starts with, and the C that opens
// CONTROL halfway through.
var initials = map[int]bool{0: true, 7: true}

// styWordmark is the wordmark's ordinary weight; styInitial is the purple the
// M and the C wear, the same purple as the logo's lenses.
var (
	styWordmark = lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Bold(true)
	styInitial  = lipgloss.NewStyle().Foreground(colLens).Bold(true)
)

// wordmark renders MISSIONCONTROL, the M and the C in purple. Each glyph is
// styled on its own rather than the row being sliced by column, so changing a
// letter cannot silently shift the colouring onto the wrong one.
func wordmark() string {
	rows := make([]string, 5)
	for i, r := range word {
		g, ok := glyphs[r]
		if !ok {
			continue
		}
		sty := styWordmark
		if initials[i] {
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
