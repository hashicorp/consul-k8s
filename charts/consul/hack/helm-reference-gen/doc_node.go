package main

import (
	"errors"
	"fmt"
	"strings"
)

const UnknownKindError = "unknown kind"

// DocNode is a node in the final generated reference document.
// For example this would be a single DocNode:
// ```
// - `global` ((#v-global)) - Holds values that affect multiple components of the chart.
// ```
type DocNode struct {
	// Column is the character column (i.e. indent) this node should be displayed
	// at.
	// For example, if this is a root node, then its column will be 0 because it
	// shouldn't be indented.
	Column int

	// ParentBreadcrumb is the path to this node's parent from the root.
	// It is used for the HTML anchor, e.g. `#v-global-name`.
	// If this node were global.name, then this would be set to "global".
	ParentBreadcrumb string

	// ParentWasMap is true when the parent of this node was a map.
	ParentWasMap bool

	// Key is the key of this node, e.g. if `key: value` then Key would be "key".
	Key string

	// Default is the default value for this node, e.g. if key defaults to false,
	// Default would be "false".
	Default string

	// Comment is the YAML comment that described this node.
	Comment string

	// KindTag is the YAML parsed kind tag from the YAML library. This has values
	// like "!!seq" and "!!str".
	KindTag string

	// Children are other nodes that should be displayed as sub-keys of this node.
	Children []DocNode
}

// Validate returns an error if this node is invalid, else nil.
func (n DocNode) Validate() error {
	kind := n.FormattedKind()
	if strings.Contains(kind, UnknownKindError) {
		return errors.New(kind)
	}
	return nil
}

// HTMLAnchor constructs the HTML anchor to be used to link to this node.
func (n DocNode) HTMLAnchor() string {
	return fmt.Sprintf("%s-%s", n.ParentBreadcrumb, strings.ToLower(n.Key))
}

// FormattedDefault returns the default value for this node formatted properly.
func (n DocNode) FormattedDefault() string {

	// Check for the annotation first.
	if match := defaultAnnotation.FindAllStringSubmatch(n.Comment, -1); len(match) > 0 {
		// Handle it being set > 1 time. Use the last match.
		return match[len(match)-1][1]
	}

	// We don't show the default if the kind is a map of arrays or map because the
	// default will be too big to show inline.
	if n.FormattedKind() == "array<map>" || n.FormattedKind() == "map" {
		return ""
	}

	if n.Default != "" {
		// Don't show multiline string defaults since it wouldn't fit.
		// We use > 2 because if it's extraConfig, e.g. `{}` then we want to
		// show it but if it's affinity then it doesn't make sense to show it.
		if len(strings.Split(n.Default, "\n")) > 2 {
			return ""
		}
		return strings.TrimSpace(n.Default)
	}

	// If we get here then the default is an empty string. We return quotes
	// in this case so it's clear it's an empty string. Otherwise it would look
	// like: `string: ` vs. `string: ""`.
	return `""`
}

// FormattedDocumentation returns the formatted documentation for this node.
func (n DocNode) FormattedDocumentation() string {
	doc := n.Comment

	// Replace all leading YAML comment characters, e.g.
	// `# yaml comment` => `yaml comment`.
	doc = commentPrefix.ReplaceAllString(n.Comment, "")

	// Indent each line of the documentation so it lines up correctly.
	var indentedLines []string
	for i, line := range strings.Split(doc, "\n") {

		// If the line is a @type, @default or @recurse annotation we don't include it in
		// the markdown description.
		// This check must be before the i == 0 check because if there's only
		// one line in the description and it's the type description then we
		// want to discard it.
		if len(typeAnnotation.FindStringSubmatch(line)) > 0 ||
			len(defaultAnnotation.FindStringSubmatch(line)) > 0 ||
			len(recurseAnnotation.FindStringSubmatch(line)) > 0 {
			continue
		}

		var indentedLine string
		// The first line is printed inline with the key information so it
		// doesn't need to be indented, e.g.
		// `key - first line docs`
		if i == 0 {
			indentedLine = line
		} else if line != "" {
			indent := n.Column + 1
			if n.ParentWasMap {
				indent = n.Column
			}
			indentedLine = strings.Repeat(" ", indent) + line
		} else {
			// No need to add whitespace indent to a newline.
		}
		indentedLines = append(indentedLines, indentedLine)
	}

	// Trim all final newlines and whitespace.
	return strings.TrimRight(strings.Join(indentedLines, "\n"), "\n ")
}

// FormattedKind returns the kind of this node, e.g. string, boolean, etc.
func (n DocNode) FormattedKind() string {

	// Check for the annotation first.
	if match := typeAnnotation.FindAllStringSubmatch(n.Comment, -1); len(match) > 0 {
		// Handle it being set > 1 time. Use the last match.
		return match[len(match)-1][1]
	}

	// Special case for secretName, secretKey so they don't need to set
	// # type: string.
	if n.Key == "secretName" || n.Key == "secretKey" {
		return "string"
	}

	// The YAML kind tag looks like "!!str".
	switch strings.TrimLeft(n.KindTag, "!") {
	case "str":
		return "string"
	case "int":
		return "integer"
	case "bool":
		return "boolean"
	case "map":
		// We don't show the kind if its of type because it's obvious it's a map
		// because it will have subkeys and so showing the type as map would
		// just complicate reading without any benefit.
		// NOTE: If it's been explicitly annotated with @type: map then we
		// will show it as that is handled above via the typeAnnotation regex
		// match.
		return ""
	default:
		return fmt.Sprintf("%s '%v'", UnknownKindError, n.KindTag)
	}
}

// LeadingIndent returns the leading indentation for the first line of this
// node.
func (n DocNode) LeadingIndent() string {
	indent := n.Column - 1
	if n.ParentWasMap {
		indent = n.Column - 3
	}
	return strings.Repeat(" ", indent)
}
