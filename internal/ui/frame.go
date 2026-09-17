package ui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// The panes that float over the screen, the ? overlay and the settings, share
// a frame and a way of being drawn over everything else.

// floating reports whether there is room for a pane to float with a margin
// around it. Below the overlay's thresholds it takes the whole terminal
// instead, because a margin there costs more room than it is worth.
func (a *App) floating() bool {
	return a.width >= overlayFullWidth && a.height >= overlayFullHeight
}

// floatOver draws box, width columns wide, over a finished screen with its top
// left corner at x, y. The screen behind loses its colour, so the box reads as
// the thing in front, and each box line is cut into the line it covers rather
// than composited, which keeps the result exactly the size of the terminal.
func (a *App) floatOver(base, box []string, x, y, width int) []string {
	dim := a.styles.Backdrop

	out := make([]string, a.height)
	for row := range out {
		line := ""
		if row < len(base) {
			line = ansi.Strip(base[row])
		}
		line = padTo(line, a.width)

		i := row - y
		if i < 0 || i >= len(box) {
			out[row] = dim.Render(ansi.Cut(line, 0, a.width))
			continue
		}
		out[row] = dim.Render(ansi.Cut(line, 0, x)) +
			ansi.Truncate(box[i], max(a.width-x, 0), "") +
			dim.Render(ansi.Cut(line, x+width, a.width))
	}
	return out
}

// frameRow draws one line inside a frame: the sides, a space of padding each
// side, and content cut or padded to inner columns.
func (a *App) frameRow(content string, inner int) string {
	side := a.styles.OverlayBorder.Render("│")
	return side + " " + padTo(ansi.Truncate(content, inner, ellipsis), inner) + " " + side
}

// frameRule draws a divider across a frame width columns wide.
func (a *App) frameRule(width int) string {
	return a.styles.OverlayBorder.Render("├" + strings.Repeat("─", max(width-2, 0)) + "┤")
}
