package flags

import "strings"

// Taken from https://github.com/hashicorp/consul/blob/b5b9c8d953cd3c79c6b795946839f4cf5012f507/command/flags/flag_slice_value.go
// This was done so we don't depend on internal Consul implementation.

// AppendSliceValue implements the flag.Value interface and allows multiple
// calls to the same variable to append a list.
type AppendSliceValue []string

func (s *AppendSliceValue) String() string {
	return strings.Join(*s, ",")
}

func (s *AppendSliceValue) Set(value string) error {
	if *s == nil {
		*s = make([]string, 0, 1)
	}

	*s = append(*s, value)
	return nil
}
