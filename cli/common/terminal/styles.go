package terminal

import (
	"io"

	"github.com/fatih/color"
)

const (
	HeaderStyle        = "header"
	ErrorStyle         = "error"
	ErrorBoldStyle     = "error-bold"
	WarningStyle       = "warning"
	WarningBoldStyle   = "warning-bold"
	InfoStyle          = "info"
	LibraryStyle       = "library"
	SuccessStyle       = "success"
	SuccessBoldStyle   = "success-bold"
	DiffUnchangedStyle = "diff-unchanged"
	DiffAddedStyle     = "diff-added"
	DiffRemovedStyle   = "diff-removed"
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

type config struct {
	// Writer is where the message will be written to.
	Writer io.Writer

	// The style the output should take on
	Style string
}

// Option controls output styling.
type Option func(*config)

// WithHeaderStyle styles the output like a header denoting a new section
// of execution. This should only be used with single-line output. Multi-line
// output will not look correct.
func WithHeaderStyle() Option {
	return func(c *config) {
		c.Style = HeaderStyle
	}
}

// WithInfoStyle styles the output like it's formatted information.
func WithInfoStyle() Option {
	return func(c *config) {
		c.Style = InfoStyle
	}
}

// WithErrorStyle styles the output as an error message.
func WithErrorStyle() Option {
	return func(c *config) {
		c.Style = ErrorStyle
	}
}

// WithWarningStyle styles the output as an warning message.
func WithWarningStyle() Option {
	return func(c *config) {
		c.Style = WarningStyle
	}
}

// WithSuccessStyle styles the output as a success message.
func WithSuccessStyle() Option {
	return func(c *config) {
		c.Style = SuccessStyle
	}
}

// WithLibraryStyle styles the output with an arrow pointing to a section.
func WithLibraryStyle() Option {
	return func(c *config) {
		c.Style = LibraryStyle
	}
}

// WithDiffUnchangedStyle colors the diff style in white.
func WithDiffUnchangedStyle() Option {
	return func(c *config) {
		c.Style = DiffUnchangedStyle
	}
}

// WithDiffAddedStyle colors the output in green.
func WithDiffAddedStyle() Option {
	return func(c *config) {
		c.Style = DiffAddedStyle
	}
}

// WithDiffRemovedStyle colors the output in red.
func WithDiffRemovedStyle() Option {
	return func(c *config) {
		c.Style = DiffRemovedStyle
	}
}

// WithStyle allows for setting a style by passing a string.
func WithStyle(style string) Option {
	return func(c *config) {
		c.Style = style
	}
}

// WithWriter specifies the writer for the output.
func WithWriter(w io.Writer) Option {
	return func(c *config) { c.Writer = w }
}
