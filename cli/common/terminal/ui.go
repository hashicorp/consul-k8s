// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package terminal

import (
	"errors"
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

// ErrNonInteractive is returned when Input is called on a non-Interactive UI.
var ErrNonInteractive = errors.New("noninteractive UI doesn't support this operation")

// Passed to UI.NamedValues to provide a nicely formatted key: value output.
type NamedValue struct {
	Name  string
	Value interface{}
}

// UI is the primary interface for interacting with a user via the CLI.
//
// Some of the methods on this interface return values that have a lifetime
// such as Status and StepGroup. While these are still active (haven't called
// the close or equivalent method on these values), no other method on the
// UI should be called.
type UI interface {
	// Input asks the user for input. This will immediately return an error
	// if the UI doesn't support interaction. You can test for interaction
	// ahead of time with Interactive().
	Input(*Input) (string, error)

	// Interactive returns true if this prompt supports user interaction.
	// If this is false, Input will always error.
	Interactive() bool

	// Output outputs a message directly to the terminal. The remaining
	// arguments should be interpolations for the format string. After the
	// interpolations you may add Options.
	Output(string, ...interface{})

	// Output data as a table of data. Each entry is a row which will be output
	// with the columns lined up nicely.
	NamedValues([]NamedValue, ...Option)

	// OutputWriters returns stdout and stderr writers. These are usually
	// but not always TTYs. This is useful for subprocesses, network requests,
	// etc. Note that writing to these is not thread-safe by default so
	// you must take care that there is only ever one writer.
	OutputWriters() (stdout, stderr io.Writer, err error)

	// Table outputs the information formatted into a Table structure.
	Table(*Table, ...Option)
}

// Input is the configuration for an input.
type Input struct {
	// Prompt is a single-line prompt to give the user such as "Continue?"
	// The user will input their answer after this prompt.
	Prompt string

	// Style is the style to apply to the input. If this is blank,
	// the output won't be colorized in any way.
	Style string

	// True if this input is a secret. The input will be masked.
	Secret bool
}

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
