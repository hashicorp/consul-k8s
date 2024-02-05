package flags

import (
	"fmt"
	"testing"
)

// Taken from https://github.com/hashicorp/consul/blob/35daee45bc3bf9fdce5845f2219576e861b23f40/command/flags/flag_map_value_test.go
// This was done so we don't depend on internal Consul implementation.

func TestFlagMapValueSet(t *testing.T) {
	t.Parallel()

	t.Run("missing =", func(t *testing.T) {

		f := new(FlagMapValue)
		if err := f.Set("foo"); err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("sets", func(t *testing.T) {

		f := new(FlagMapValue)
		if err := f.Set("foo=bar"); err != nil {
			t.Fatal(err)
		}

		r, ok := (*f)["foo"]
		if !ok {
			t.Errorf("missing value: %#v", f)
		}
		if exp := "bar"; r != exp {
			t.Errorf("expected %q to be %q", r, exp)
		}
	})

	t.Run("sets multiple", func(t *testing.T) {

		f := new(FlagMapValue)

		r := map[string]string{
			"foo": "bar",
			"zip": "zap",
			"cat": "dog",
		}

		for k, v := range r {
			if err := f.Set(fmt.Sprintf("%s=%s", k, v)); err != nil {
				t.Fatal(err)
			}
		}

		for k, v := range r {
			r, ok := (*f)[k]
			if !ok {
				t.Errorf("missing value %q: %#v", k, f)
			}
			if exp := v; r != exp {
				t.Errorf("expected %q to be %q", r, exp)
			}
		}
	})

	t.Run("overwrites", func(t *testing.T) {

		f := new(FlagMapValue)
		if err := f.Set("foo=bar"); err != nil {
			t.Fatal(err)
		}
		if err := f.Set("foo=zip"); err != nil {
			t.Fatal(err)
		}

		r, ok := (*f)["foo"]
		if !ok {
			t.Errorf("missing value: %#v", f)
		}
		if exp := "zip"; r != exp {
			t.Errorf("expected %q to be %q", r, exp)
		}
	})
}
