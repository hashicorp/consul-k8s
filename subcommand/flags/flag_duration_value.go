package flags

import (
	"reflect"
	"time"

	"github.com/mitchellh/mapstructure"
)

// Taken from https://github.com/hashicorp/consul/blob/master/command/flags/flag_duration_value.go
// This was done so we don't depend on internal Consul implementation.

// DurationValue provides a flag value that's aware if it has been set.
type DurationValue struct {
	v *time.Duration
}

// Merge will overlay this value if it has been set.
func (d *DurationValue) Merge(onto *time.Duration) {
	if d.v != nil {
		*onto = *(d.v)
	}
}

// Set implements the flag.Value interface.
func (d *DurationValue) Set(v string) error {
	if d.v == nil {
		d.v = new(time.Duration)
	}
	var err error
	*(d.v), err = time.ParseDuration(v)
	return err
}

// String implements the flag.Value interface.
func (d *DurationValue) String() string {
	var current time.Duration
	if d.v != nil {
		current = *(d.v)
	}
	return current.String()
}

// StringToDurationValueFunc is a mapstructure hook that looks for an incoming
// string mapped to a DurationValue and does the translation.
func StringToDurationValueFunc() mapstructure.DecodeHookFunc {
	return func(
		f reflect.Type,
		t reflect.Type,
		data interface{}) (interface{}, error) {
		if f.Kind() != reflect.String {
			return data, nil
		}

		val := DurationValue{}
		if t != reflect.TypeOf(val) {
			return data, nil
		}
		if err := val.Set(data.(string)); err != nil {
			return nil, err
		}
		return val, nil
	}
}
