package main

import "fmt"

// ParseError is an error that occurs during parsing.
// It's used to include information about which node failed to parse.
type ParseError struct {
	ParentAnchor string
	CurrAnchor   string
	FullAnchor   string
	Err          string
}

func (p *ParseError) Error() string {
	anchor := p.FullAnchor
	if anchor == "" {
		anchor = fmt.Sprintf("%s-%s", p.ParentAnchor, p.CurrAnchor)
	}
	return fmt.Sprintf("%s: %s", anchor, p.Err)
}
