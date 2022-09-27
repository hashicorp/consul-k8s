package parsetags

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseTags(t *testing.T) {
	cases := []struct {
		tagsAnno string
		exp      []string
	}{
		{
			"tag",
			[]string{"tag"},
		},
		{
			",,removes,,empty,elems,,",
			[]string{"removes", "empty", "elems"},
		},
		{
			"removes , white  ,space ",
			[]string{"removes", "white", "space"},
		},
		{
			`\,leading,comma`,
			[]string{",leading", "comma"},
		},
		{
			`trailing,comma\,`,
			[]string{"trailing", "comma,"},
		},
		{
			`mid\,dle,com\,ma`,
			[]string{"mid,dle", "com,ma"},
		},
		{
			`\,\,multi\,\,,\,com\,\,ma`,
			[]string{",,multi,,", ",com,,ma"},
		},
		{
			`  every\,\,   ,  thing  `,
			[]string{"every,,", "thing"},
		},
	}

	for _, c := range cases {
		require.Equal(t, c.exp, ParseTags(c.tagsAnno))
	}
}
