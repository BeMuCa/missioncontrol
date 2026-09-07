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

// The mark is one line across, not two stacked words, and every row of it is
// the full declared width — a short row would leave a notch in a letter.
func TestWordmarkIsOneLineAtDeclaredWidth(t *testing.T) {
	rows := strings.Split(wordmark(), "\n")
	if len(rows) != wordmarkRows {
		t.Fatalf("%d rows, want %d — the mark must be one line across, not stacked", len(rows), wordmarkRows)
	}
	for i, l := range rows {
		if got := lipgloss.Width(l); got != wordmarkWidth {
			t.Errorf("wordmark row %d is %d columns wide, want %d", i, got, wordmarkWidth)
		}
	}
	t.Log("\n" + wordmark())
}

// Both initials wear the purple and nothing else does: the M it opens with and
// the C that starts CONTROL.
func TestWordmarkColoursBothInitials(t *testing.T) {
	if len(word) != 14 {
		t.Fatalf("word = %q, want the fourteen letters the widths are computed from", word)
	}
	if !initials[0] || !initials[7] {
		t.Errorf("initials = %v, want the M at 0 and the C at 7", initials)
	}
	if word[0] != 'M' || word[7] != 'C' {
		t.Errorf("word[0]=%q word[7]=%q, want M and C", word[0], word[7])
	}
	if len(initials) != 2 {
		t.Errorf("%d coloured letters, want exactly the two initials", len(initials))
	}
}
