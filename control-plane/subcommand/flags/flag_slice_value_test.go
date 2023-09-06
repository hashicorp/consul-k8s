package flags

import (
	"flag"
	"reflect"
	"testing"
)

// Taken from https://github.com/hashicorp/consul/blob/b5b9c8d953cd3c79c6b795946839f4cf5012f507/command/flags/flag_slice_value_test.go
// This was done so we don't depend on internal Consul implementation.

func TestAppendSliceValue_implements(t *testing.T) {
	t.Parallel()
	var raw interface{} = new(AppendSliceValue)
	if _, ok := raw.(flag.Value); !ok {
		t.Fatalf("AppendSliceValue should be a Value")
	}
}

func TestAppendSliceValueSet(t *testing.T) {
	t.Parallel()
	sv := new(AppendSliceValue)
	err := sv.Set("foo")
	if err != nil {
		t.Fatalf("err: %s", err)
	}

	err = sv.Set("bar")
	if err != nil {
		t.Fatalf("err: %s", err)
	}

	expected := []string{"foo", "bar"}
	if !reflect.DeepEqual([]string(*sv), expected) {
		t.Fatalf("Bad: %#v", sv)
	}
}
