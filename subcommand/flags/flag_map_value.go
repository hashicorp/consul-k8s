package flags

import (
	"flag"
	"fmt"
	"strings"
)

// Taken from https://github.com/hashicorp/consul/blob/35daee45bc3bf9fdce5845f2219576e861b23f40/command/flags/flag_map_value.go
// This was done so we don't depend on internal Consul implementation.

// Ensure implements
var _ flag.Value = (*FlagMapValue)(nil)

// FlagMapValue is a flag implementation used to provide key=value semantics
// multiple times.
type FlagMapValue map[string]string

func (h *FlagMapValue) String() string {
	return fmt.Sprintf("%v", *h)
}

func (h *FlagMapValue) Set(value string) error {
	idx := strings.Index(value, "=")
	if idx == -1 {
		return fmt.Errorf("Missing \"=\" value in argument: %s", value)
	}

	key, value := value[0:idx], value[idx+1:]

	if *h == nil {
		*h = make(map[string]string)
	}

	headers := *h
	headers[key] = value
	*h = headers

	return nil
}
