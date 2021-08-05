package flags

import "github.com/deckarep/golang-set"

// ToSet creates a set from s.
func ToSet(s []string) mapset.Set {
	set := mapset.NewSet()
	for _, allow := range s {
		set.Add(allow)
	}
	return set
}
