package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

// The art is edited by hand, so the one thing worth asserting is that it is
// still rectangular: a short row would shift every pixel after it.
func TestLogoPixelMapIsRectangular(t *testing.T) {
	if len(logoPixels)%2 != 0 {
		t.Fatalf("%d pixel rows, want an even count so every row pairs into a half block", len(logoPixels))
	}
	for i, row := range logoPixels {
		if len([]rune(row)) != logoWidth {
			t.Errorf("row %d is %d columns, want %d", i, len([]rune(row)), logoWidth)
		}
	}
}

func TestLogoArtRendersAtDeclaredSize(t *testing.T) {
	art := logoArt()
	lines := strings.Split(art, "\n")
	if want := len(logoPixels) / 2; len(lines) != want {
		t.Fatalf("%d text rows, want %d", len(lines), want)
	}
	for i, l := range lines {
		if w := lipgloss.Width(l); w != logoWidth {
			t.Errorf("rendered row %d is %d columns wide, want %d", i, w, logoWidth)
		}
	}
	t.Log("\n" + art)
}

func TestWordmarkRendersBothWordsAtDeclaredWidth(t *testing.T) {
	w := wordmark()
	for i, l := range strings.Split(w, "\n") {
		if got := lipgloss.Width(l); got != 0 && got != wordmarkWidth {
			t.Errorf("wordmark row %d is %d columns wide, want %d", i, got, wordmarkWidth)
		}
	}
	t.Log("\n" + w)
}
