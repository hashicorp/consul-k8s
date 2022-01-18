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

// basicUI
type basicUI struct {
	ctx context.Context
}

func NewBasicUI(ctx context.Context) *basicUI {
	return &basicUI{
		ctx: ctx,
	}
}

// Input implements UI
func (ui *basicUI) Input(input *Input) (string, error) {
	var buf bytes.Buffer

	// Write the prompt, add a space.
	ui.Output(input.Prompt, WithStyle(input.Style), WithWriter(&buf))
	fmt.Fprint(color.Output, strings.TrimRight(buf.String(), "\r\n"))
	fmt.Fprint(color.Output, " ")

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
		fmt.Fprintln(color.Output)
		return "", ui.ctx.Err()
	}
}

// Interactive implements UI
func (ui *basicUI) Interactive() bool {
	return isatty.IsTerminal(os.Stdin.Fd())
}

// Output implements UI
func (ui *basicUI) Output(msg string, raw ...interface{}) {
	msg, style, w := Interpret(msg, raw...)

	switch style {
	case HeaderStyle:
		msg = colorHeader.Sprintf("\n==> %s", msg)
	case ErrorStyle:
		msg = colorError.Sprintf(" ! %s", msg)
	case ErrorBoldStyle:
		msg = colorErrorBold.Sprintf(" ! %s", msg)
	case WarningStyle:
		msg = colorWarning.Sprintf(" * %s", msg)
	case WarningBoldStyle:
		msg = colorWarningBold.Sprintf(" * %s", msg)
	case SuccessStyle:
		msg = colorSuccess.Sprintf(" ✓ %s", msg)
	case SuccessBoldStyle:
		msg = colorSuccessBold.Sprintf(" ✓ %s", msg)
	case LibraryStyle:
		msg = colorLibrary.Sprintf(" --> %s", msg)
	case DiffAddedStyle:
		msg = colorDiffAdded.Sprintf("%s", msg)
	case DiffRemovedStyle:
		msg = colorDiffRemoved.Sprintf("%s", msg)
	case InfoStyle:
		lines := strings.Split(msg, "\n")
		for i, line := range lines {
			lines[i] = colorInfo.Sprintf("    %s", line)
		}

		msg = strings.Join(lines, "\n")
	}

	// Write it
	fmt.Fprintln(w, msg)
}

// NamedValues implements UI
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

// OutputWriters implements UI
func (ui *basicUI) OutputWriters() (io.Writer, io.Writer, error) {
	return os.Stdout, os.Stderr, nil
}
