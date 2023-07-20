package flag

import (
	"regexp"
	"strings"

	"github.com/kr/text"
)

// maxLineLength is the maximum width of any line.
const maxLineLength int = 78

// reRemoveWhitespace is a regular expression for stripping whitespace from
// a string.
var reRemoveWhitespace = regexp.MustCompile(`[\s]+`)

// FlagExample is an interface which declares an example value. This is
// used in help generation to provide better help text.
type FlagExample interface {
	Example() string
}

// FlagVisibility is an interface which declares whether a flag should be
// hidden from help and completions. This is usually used for deprecations
// on "internal-only" flags.
type FlagVisibility interface {
	Hidden() bool
}

// wrapAtLengthWithPadding wraps the given text at the maxLineLength, taking
// into account any provided left padding.
func wrapAtLengthWithPadding(s string, pad int) string {
	wrapped := text.Wrap(s, maxLineLength-pad)
	lines := strings.Split(wrapped, "\n")
	for i, line := range lines {
		lines[i] = strings.Repeat(" ", pad) + line
	}

	return strings.Join(lines, "\n")
}
