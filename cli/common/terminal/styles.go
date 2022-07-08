package terminal

import (
	"io"
	"strings"

	"github.com/fatih/color"
)

// config configures the style and buffer for the output to the terminal.
type config struct {
	// style determines what the output will look like.
	style Style

	// bufOut provides an override for the standard output buffer.
	bufOut io.Writer
}

// The style the for output to the terminal.
type Style string

const (
	Default       Style = ""
	Header        Style = "header"
	Error         Style = "error"
	ErrorBold     Style = "error-bold"
	Warning       Style = "warning"
	WarningBold   Style = "warning-bold"
	Info          Style = "info"
	Library       Style = "library"
	Success       Style = "success"
	SuccessBold   Style = "success-bold"
	DiffUnchanged Style = "diff-unchanged"
	DiffAdded     Style = "diff-added"
	DiffRemoved   Style = "diff-removed"
)

var (
	colorHeader        = color.New(color.Bold)
	colorInfo          = color.New()
	colorError         = color.New(color.FgRed)
	colorErrorBold     = color.New(color.FgRed, color.Bold)
	colorLibrary       = color.New(color.FgCyan)
	colorSuccess       = color.New(color.FgGreen)
	colorSuccessBold   = color.New(color.FgGreen, color.Bold)
	colorWarning       = color.New(color.FgYellow)
	colorWarningBold   = color.New(color.FgYellow, color.Bold)
	colorDiffUnchanged = color.New()
	colorDiffAdded     = color.New(color.FgGreen)
	colorDiffRemoved   = color.New(color.FgRed)
)

// FormatStyle takes a message and the desired style and modifies the message
// to properly match the desired style. If no matching style is found, the
// original message is returned.
func FormatStyle(msg string, style Style) string {
	switch style {
	case Header:
		return colorHeader.Sprintf("\n==> %s", msg)
	case Error:
		return colorError.Sprintf(" ! %s", msg)
	case ErrorBold:
		return colorErrorBold.Sprintf(" ! %s", msg)
	case Warning:
		return colorWarning.Sprintf(" * %s", msg)
	case WarningBold:
		return colorWarningBold.Sprintf(" * %s", msg)
	case Success:
		return colorSuccess.Sprintf(" ✓ %s", msg)
	case SuccessBold:
		return colorSuccessBold.Sprintf(" ✓ %s", msg)
	case Library:
		return colorLibrary.Sprintf(" --> %s", msg)
	case DiffUnchanged:
		return colorDiffUnchanged.Sprintf("  %s", msg)
	case DiffAdded:
		return colorDiffAdded.Sprintf("  %s", msg)
	case DiffRemoved:
		return colorDiffRemoved.Sprintf("  %s", msg)
	case Info:
		lines := strings.Split(msg, "\n")
		for i, line := range lines {
			lines[i] = colorInfo.Sprintf("    %s", line)
		}

		return strings.Join(lines, "\n")
	default:
		return msg
	}
}

// Option controls output styling.
type Option func(*config)

// WithStyle allows for setting a style by passing a string.
func WithStyle(style Style) Option {
	return func(c *config) {
		c.style = style
	}
}

func WithWriter(buf *io.Writer) Option {
	return func(c *config) {
		c.bufOut = *buf
	}
}

// WithHeaderStyle styles the output like a header denoting a new section
// of execution. This should only be used with single-line output. Multi-line
// output will not look correct.
func WithHeaderStyle() Option {
	return func(c *config) {
		c.style = Header
	}
}

// WithInfoStyle styles the output like it's formatted information.
func WithInfoStyle() Option {
	return func(c *config) {
		c.style = Header
	}
}

// WithErrorStyle styles the output as an error message.
func WithErrorStyle() Option {
	return func(c *config) {
		WithStyle(Error)
	}
}

// WithWarningStyle styles the output as an warning message.
func WithWarningStyle() Option {
	return func(outStyle *Style) {
		WithStyle(Warning)
	}
}

// WithSuccessStyle styles the output as a success message.
func WithSuccessStyle() Option {
	return func(outStyle *Style) {
		WithStyle(Success)
	}
}

// WithLibraryStyle styles the output with an arrow pointing to a section.
func WithLibraryStyle() Option {
	return func(outStyle *Style) {
		WithStyle(Library)
	}
}

// WithDiffUnchangedStyle colors the diff style in white.
func WithDiffUnchangedStyle() Option {
	return func(outStyle *Style) {
		WithStyle(DiffUnchanged)
	}
}

// WithDiffAddedStyle colors the output in green.
func WithDiffAddedStyle() Option {
	return func(outStyle *Style) {
		WithStyle(DiffAdded)
	}
}

// WithDiffRemovedStyle colors the output in red.
func WithDiffRemovedStyle() Option {
	return func(outStyle *Style) {
		WithStyle(DiffRemoved)
	}
}
