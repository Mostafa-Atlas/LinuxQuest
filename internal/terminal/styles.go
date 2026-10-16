package terminal

import "github.com/charmbracelet/lipgloss"

// Styles holds the (deliberately subtle) styling. It is built from a
// per-connection renderer so colours match each client's terminal.
type Styles struct {
	Title, Dim, Accent, Ok, Err, Hint, Prompt, Selected, Box lipgloss.Style
}

// NewStyles builds styles for the given renderer (nil uses the default).
func NewStyles(r *lipgloss.Renderer) Styles {
	if r == nil {
		r = lipgloss.DefaultRenderer()
	}
	n := r.NewStyle
	return Styles{
		Title:    n().Bold(true).Foreground(lipgloss.Color("14")),
		Dim:      n().Foreground(lipgloss.Color("8")),
		Accent:   n().Foreground(lipgloss.Color("6")),
		Ok:       n().Foreground(lipgloss.Color("10")),
		Err:      n().Foreground(lipgloss.Color("9")),
		Hint:     n().Foreground(lipgloss.Color("11")),
		Prompt:   n().Bold(true).Foreground(lipgloss.Color("10")),
		Selected: n().Bold(true).Foreground(lipgloss.Color("14")),
		Box:      n().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("8")).Padding(0, 1),
	}
}
