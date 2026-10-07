package store_test

import (
	"context"
	"slices"
	"testing"

	"tft-platform/internal/store/storetest"
)

// Every insert of a participant updates each index on match_participants, so
// the table keeps only the ones queries use (migration 0013).
func TestMatchParticipantsIndexes(t *testing.T) {
	st := storetest.New(t)
	rows, err := st.Pool.Query(context.Background(), `
		SELECT indexname FROM pg_indexes
		WHERE tablename = 'match_participants' AND schemaname = current_schema()
		ORDER BY indexname`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		got = append(got, name)
	}
	want := []string{
		"idx_participants_puuid",
		"idx_participants_traits_gin",
		"idx_participants_units_gin",
		"match_participants_match_id_puuid_key",
		"match_participants_pkey",
	}
	if !slices.Equal(got, want) {
		t.Errorf("indexes on match_participants = %v, want %v", got, want)
	}
}
