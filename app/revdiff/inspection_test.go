package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLSPInspectorAvailability(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want bool
	}{
		{nil, true},
		{[]string{"--all-files"}, true},
		{[]string{"--only", "main.go"}, true},
		{[]string{"HEAD~1"}, false},
		{[]string{"main", "topic"}, false},
		{[]string{"--stdin"}, false},
	} {
		opts, err := parseArgs(tc.args)
		require.NoError(t, err)
		client := newLSPInspector(opts, t.TempDir())
		require.Equal(t, tc.want, client != nil, "args: %v", tc.args)
		if client != nil {
			require.NoError(t, client.Close())
		}
	}
	require.Nil(t, newLSPInspector(options{CompareOld: "a", CompareNew: "b"}, t.TempDir()))
	require.Nil(t, newLSPInspector(options{}, ""))
}
