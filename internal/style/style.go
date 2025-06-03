// Package style provides a thin wrapper for managing ANSI escape sequences
// for styled terminal output.
package style

import "github.com/fatih/color"

// Style manages ANSI escape sequences for styled output.
type Style struct {
	bold   *color.Color
	green  *color.Color
	red    *color.Color
	yellow *color.Color
}

// New creates a new [Style] instance with ANSI escape sequences explicitly
// enabled or disabled, based on the fancy param.
func New(fancy bool) *Style {
	s := &Style{
		bold:   color.New(color.Bold),
		green:  color.New(color.FgGreen),
		red:    color.New(color.FgRed),
		yellow: color.New(color.FgYellow),
	}
	if fancy {
		s.bold.EnableColor()
		s.green.EnableColor()
		s.red.EnableColor()
		s.yellow.EnableColor()
	} else {
		s.bold.DisableColor()
		s.green.DisableColor()
		s.red.DisableColor()
		s.yellow.DisableColor()
	}
	return s
}

// Bold formats its args as bold text.
func (s *Style) Bold(a ...any) string {
	return s.bold.Sprint(a...)
}

// Boldf formats its args as bold text.
func (s *Style) Boldf(format string, a ...any) string {
	return s.bold.Sprintf(format, a...)
}

// Green formats its args as green text.
func (s *Style) Green(a ...any) string {
	return s.green.Sprint(a...)
}

// Greenf formats its args as green text.
func (s *Style) Greenf(format string, a ...any) string {
	return s.green.Sprintf(format, a...)
}

// Red formats its args as red text.
func (s *Style) Red(a ...any) string {
	return s.red.Sprint(a...)
}

// Redf formats its args as red text.
func (s *Style) Redf(format string, a ...any) string {
	return s.red.Sprintf(format, a...)
}

// Yellow formats its args as yellow text.
func (s *Style) Yellow(a ...any) string {
	return s.yellow.Sprint(a...)
}

// Yellowf formats its args as yellow text.
func (s *Style) Yellowf(format string, a ...any) string {
	return s.yellow.Sprintf(format, a...)
}
