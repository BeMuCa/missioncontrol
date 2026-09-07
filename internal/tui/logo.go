package tui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
)

// The launcher's mark: an agent's head — round, thick glasses, an earpiece.
//
// It is kept as a pixel map rather than as pre-rendered escape codes so it can
// be edited by looking at it. Two pixel rows share one text row through the
// upper half block, which is what makes the head come out round: a half-block
// pixel is about as wide as it is tall, where a text cell is twice as tall as
// it is wide.
var logoPixels = []string{
	"............ohhhhhhho.............",
	"..........ohhhhhhhhhhho...........",
	"........ohhhhhhhhhhhhhhho.........",
	".......ohhhhhhhhhhhhhhhhho........",
	"......ohhhhhhhhhhhhhhhhhhho.......",
	".....ohhhhhhhhhhhhhhhhhhhhho......",
	"....oGGGGGGGGGGGGGGGGGGGGGGGo.....",
	"....oGLLLLLLLLLGGGGLLLLLLLLLG.....",
	"....oGLLLLLLLLLGGGGLLLLLLLLLGEE...",
	"....oGLLLLLLLLLGGGGLLLLLLLLLGEEE..",
	"....oGLLLLLLLLLGGGGLLLLLLLLLGEEEp.",
	"....oGLLLLLLLLLGGGGLLLLLLLLLGEEE..",
	"....oGGGGGGGGGGGGGGGGGGGGGGGGEE...",
	"....ohhhhhhhhhhhhhhhhhhhhhhhoE....",
	".....ohhhhhhhhhhhhhhhhhhhhho......",
	"......ohhhhhhhhhhhhhhhhhhho.......",
	".......ohhhhhmmmmmmmmhhhho........",
	"........ohhhhhhhhhhhhhhho.........",
	"..........ohhhhhhhhhhho...........",
	"............ohhhhhhho.............",
}

// logoWidth is the column count the map is drawn at; the launcher centres on it.
const logoWidth = 34

// logoColours maps the pixel map's keys to colours. A key with no entry is
// transparent, which is what the dots in the map are.
var logoColours = map[byte]color.Color{
	'h': colSkin,
	'o': colRim,
	'G': colFrame,
	'L': colLens,
	'E': colGear,
	'p': colLed,
	'm': colRim,
}

var (
	colSkin  = lipgloss.Color("252")
	colRim   = lipgloss.Color("245")
	colFrame = lipgloss.Color("236")
	colLens  = lipgloss.Color("141")
	colGear  = lipgloss.Color("240")
	colLed   = lipgloss.Color("177")
)

// logoArt renders the pixel map into text rows. Each row pairs two pixel rows:
// the upper half block is painted in the top pixel's colour and its background
// in the bottom one's, so a transparent pixel has to leave that side unpainted
// rather than paint it black — the launcher is drawn on the terminal's own
// background, whatever that is.
func logoArt() string {
	var out []string
	for y := 0; y+1 < len(logoPixels); y += 2 {
		top, bot := logoPixels[y], logoPixels[y+1]
		var line strings.Builder
		for x := 0; x < logoWidth; x++ {
			tc, tok := logoColours[top[x]]
			bc, bok := logoColours[bot[x]]
			switch {
			case tok && bok:
				line.WriteString(lipgloss.NewStyle().Foreground(tc).Background(bc).Render("▀"))
			case tok:
				line.WriteString(lipgloss.NewStyle().Foreground(tc).Render("▀"))
			case bok:
				line.WriteString(lipgloss.NewStyle().Foreground(bc).Render("▄"))
			default:
				line.WriteString(" ")
			}
		}
		out = append(out, line.String())
	}
	return strings.Join(out, "\n")
}
