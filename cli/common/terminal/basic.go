package terminal

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/bgentry/speakeasy"
	"github.com/fatih/color"
	"github.com/mattn/go-isatty"
)

// basicUI is the standard implementation of the UI interface for handling
// input from and output to the terminal.
type basicUI struct {
	// ctx is the context manager for the user interface.
	ctx context.Context

	// bufOut is the buffer for standard output.
	bufOut io.Writer

	// bufErr is the buffer for error output.
	bufErr io.Writer
}

// NewBasicUI creates a new instance of the standard implementation of the UI
// interface with the given context and buffers.
func NewBasicUI(ctx context.Context, bufOut, bufErr io.Writer) *basicUI {
	return &basicUI{
		ctx:    ctx,
		bufOut: bufOut,
		bufErr: bufErr,
	}
}

// Input prompts a user with the given string, waits for the user to provide
// a response and returns that response or an error.
func (ui *basicUI) Input(input *Input) (string, error) {
	var buf bytes.Buffer

	// Write the prompt, add a space.
	ui.Output(input.Prompt, WithStyle(input.Style), WithWriter(&buf))
	ui.print(strings.TrimRight(buf.String(), "\r\n "))

	// Ask for input in a go-routine so that we can ignore it.
	errCh := make(chan error, 1)
	lineCh := make(chan string, 1)
	go func() {
		var line string
		var err error
		if input.Secret && isatty.IsTerminal(os.Stdin.Fd()) {
			line, err = speakeasy.Ask("")
		} else {
			r := bufio.NewReader(os.Stdin)
			line, err = r.ReadString('\n')
		}
		if err != nil {
			errCh <- err
			return
		}

		lineCh <- strings.TrimRight(line, "\r\n")
	}()

	select {
	case err := <-errCh:
		return "", err
	case line := <-lineCh:
		return line, nil
	case <-ui.ctx.Done():
		// Print newline so that any further output starts properly
		ui.print("")
		return "", ui.ctx.Err()
	}
}

// Interactive returns true if this prompt supports user interaction.
// If this is false, Input will always error.
func (ui *basicUI) Interactive() bool {
	return isatty.IsTerminal(os.Stdin.Fd())
}

// Output prints a message directly to the terminal. The remaining
// arguments should be interpolations for the format string. After the
// interpolations you may add Options.
func (ui *basicUI) Output(msg string, raw ...interface{}) {
	msg, style := Interpret(msg, raw...)
	msg = FormatStyle(msg, style)
	ui.print(msg)
}

// NamedValues outputs data as a table of data. Each entry is a row which will be output
// with the columns lined up nicely.
func (ui *basicUI) NamedValues(rows []NamedValue, opts ...Option) {
	cfg := &config{Writer: color.Output}
	for _, opt := range opts {
		opt(cfg)
	}

	var buf bytes.Buffer
	tr := tabwriter.NewWriter(&buf, 1, 8, 0, ' ', tabwriter.AlignRight)
	for _, row := range rows {
		switch v := row.Value.(type) {
		case int, uint, int8, uint8, int16, uint16, int32, uint32, int64, uint64:
			fmt.Fprintf(tr, "  %s: \t%d\n", row.Name, row.Value)
		case float32, float64:
			fmt.Fprintf(tr, "  %s: \t%f\n", row.Name, row.Value)
		case bool:
			fmt.Fprintf(tr, "  %s: \t%v\n", row.Name, row.Value)
		case string:
			if v == "" {
				continue
			}
			fmt.Fprintf(tr, "  %s: \t%s\n", row.Name, row.Value)
		default:
			fmt.Fprintf(tr, "  %s: \t%s\n", row.Name, row.Value)
		}
	}

	_ = tr.Flush()
	_, _ = colorInfo.Fprintln(cfg.Writer, buf.String())
}

// OutputWriters returns stdout and stderr writers. These are usually
// but not always TTYs. This is useful for subprocesses, network requests,
// etc. Note that writing to these is not thread-safe by default so
// you must take care that there is only ever one writer.
func (ui *basicUI) OutputWriters() (io.Writer, io.Writer, error) {
	return ui.bufOut, ui.bufErr, nil
}

// print pushes the msg to the output buffer.
func (ui *basicUI) print(msg string) {
	fmt.Fprint(ui.bufOut, msg)
}

// println pushes the msg to the output buffer along with a newline.
func (ui *basicUI) println(msg string) {
	fmt.Fprintln(ui.bufOut, msg)
}

// printErr pushes the msg to the error buffer.
func (ui *basicUI) printErr(msg string) {
	fmt.Fprint(ui.bufErr, msg)
}
