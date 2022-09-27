package parsetags

import (
	"fmt"
	"strings"
)

// ParseTags parses the tags annotation into a slice of tags.
// Tags are split on commas (except for escaped commas "\,").
func ParseTags(tagsAnno string) []string {

	// This algorithm parses the tagsAnno string into a slice of strings.
	// Ideally we'd just split on commas but since Consul tags support commas,
	// we allow users to escape commas so they're included in the tag, e.g.
	// the annotation "tag\,with\,commas,tag2" will become the tags:
	// ["tag,with,commas", "tag2"].

	var tags []string
	// nextTag is built up char by char until we see a comma. Then we
	// append it to tags.
	var nextTag string

	for _, runeChar := range tagsAnno {
		runeStr := fmt.Sprintf("%c", runeChar)

		// Not a comma, just append to nextTag.
		if runeStr != "," {
			nextTag += runeStr
			continue
		}

		// Reached a comma but there's nothing in nextTag,
		// skip. (e.g. "a,,b" => ["a", "b"])
		if len(nextTag) == 0 {
			continue
		}

		// Check if the comma was escaped comma, e.g. "a\,b".
		if string(nextTag[len(nextTag)-1]) == `\` {
			// Replace the backslash with a comma.
			nextTag = nextTag[0:len(nextTag)-1] + ","
			continue
		}

		// Non-escaped comma. We're ready to push nextTag onto tags and reset nextTag.
		tags = append(tags, strings.TrimSpace(nextTag))
		nextTag = ""
	}

	// We're done the loop but nextTag still contains the last tag.
	if len(nextTag) > 0 {
		tags = append(tags, strings.TrimSpace(nextTag))
	}

	return tags
}
