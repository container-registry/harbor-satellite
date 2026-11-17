package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExactlyOnce(t *testing.T) {
	ok := delivery{ID: "1", Type: typePrefix + "GroupChanged", IdempotencyKey: "1", SignatureValid: true}
	require.NoError(t, exactlyOnce([]delivery{ok, {ID: "2", IdempotencyKey: "2", SignatureValid: true}}))
	require.ErrorContains(t, exactlyOnce([]delivery{ok, ok}), "event 1 was delivered 2 times")
	require.ErrorContains(t, exactlyOnce([]delivery{{ID: "3", IdempotencyKey: "3"}}), "invalid signature")
	require.ErrorContains(t, exactlyOnce([]delivery{{ID: "4", SignatureValid: true}}), "Idempotency-Key")
}

func TestMissingKindsCountsDistinctIDs(t *testing.T) {
	changed := typePrefix + "GroupChanged"
	got := []delivery{{ID: "1", Type: changed}, {ID: "1", Type: changed}, {ID: "2", Type: typePrefix + "SatelliteStale"}}
	require.Equal(t, []string{"GroupChanged (1 of 2)"}, missingKinds(got, map[string]int{"GroupChanged": 2, "SatelliteStale": 1}))
	require.Empty(t, missingKinds(got, map[string]int{"GroupChanged": 1}))
}

func TestParseWant(t *testing.T) {
	got, err := parseWant("GroupChanged=2, SatelliteStale=1")
	require.NoError(t, err)
	require.Equal(t, map[string]int{"GroupChanged": 2, "SatelliteStale": 1}, got)
	for _, bad := range []string{"", "GroupChanged", "GroupChanged=0", "GroupChanged=x"} {
		_, err := parseWant(bad)
		require.Error(t, err, bad)
	}
}
