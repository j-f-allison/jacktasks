package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// asciiLogo is the JackTasks banner shown on the startup screen.
const asciiLogo = `     ██╗ █████╗  ██████╗██╗  ██╗████████╗ █████╗ ███████╗██╗  ██╗███████╗
     ██║██╔══██╗██╔════╝██║ ██╔╝╚══██╔══╝██╔══██╗██╔════╝██║ ██╔╝██╔════╝
     ██║███████║██║     █████╔╝    ██║   ███████║███████╗█████╔╝ ███████╗
██   ██║██╔══██║██║     ██╔═██╗    ██║   ██╔══██║╚════██║██╔═██╗ ╚════██║
╚█████╔╝██║  ██║╚██████╗██║  ██╗   ██║   ██║  ██║███████║██║  ██╗███████║
 ╚════╝ ╚═╝  ╚═╝ ╚═════╝╚═╝  ╚═╝   ╚═╝   ╚═╝  ╚═╝╚══════╝╚═╝  ╚═╝╚══════╝`

const logoWidth = 72

// tokyoStops defines the gradient color stops in Tokyo Night colors:
// purple → blue → cyan.
type tokyoStop struct {
	pos     float64
	r, g, b float64
}

var tokyoStops = []tokyoStop{
	{0.00, 0xbb, 0x9a, 0xf7}, // #bb9af7 — purple
	{0.50, 0x7a, 0xa2, 0xf7}, // #7aa2f7 — blue
	{1.00, 0x7d, 0xcf, 0xff}, // #7dcfff — cyan
}

// tokyoColor returns an interpolated Tokyo Night color for position t ∈ [0,1].
func tokyoColor(t float64) lipgloss.Color {
	stops := tokyoStops
	if t <= stops[0].pos {
		s := stops[0]
		return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", uint8(s.r), uint8(s.g), uint8(s.b)))
	}
	last := stops[len(stops)-1]
	if t >= last.pos {
		return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", uint8(last.r), uint8(last.g), uint8(last.b)))
	}
	for i := 1; i < len(stops); i++ {
		if t <= stops[i].pos {
			s0, s1 := stops[i-1], stops[i]
			f := (t - s0.pos) / (s1.pos - s0.pos)
			return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x",
				uint8(s0.r+f*(s1.r-s0.r)),
				uint8(s0.g+f*(s1.g-s0.g)),
				uint8(s0.b+f*(s1.b-s0.b)),
			))
		}
	}
	return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", uint8(last.r), uint8(last.g), uint8(last.b)))
}

// renderLogo returns the logo with a left-to-right Tokyo Night gradient, or ""
// if the terminal is too narrow to fit it without wrapping.
func renderLogo(termWidth int) string {
	if termWidth > 0 && termWidth < logoWidth+2 {
		return ""
	}
	lines := strings.Split(asciiLogo, "\n")
	var b strings.Builder
	for _, line := range lines {
		runes := []rune(line)
		n := len(runes)
		b.WriteString("  ")
		for i, r := range runes {
			t := 0.0
			if n > 1 {
				t = float64(i) / float64(n-1)
			}
			b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(tokyoColor(t)).Render(string(r)))
		}
		b.WriteByte('\n')
	}
	return b.String()
}
