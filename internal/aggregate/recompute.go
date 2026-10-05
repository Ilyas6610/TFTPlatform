// Package aggregate recomputes the meta_*_stats summary tables from
// match_participants. It makes no Riot API calls and is not
// rate-limit-constrained, so it can run as often as desired, independent of
// and in parallel with the ingestion pipeline.
package aggregate

import (
	"context"
	"fmt"

	"tft-platform/internal/store"
)

type SetResult struct {
	TFTSetNumber    int
	UnitsWritten    int
	TraitsWritten   int
	AugmentsWritten int
}

// RecomputeAll rebuilds meta stats for every TFT set number represented in
// ingested matches, so a caller doesn't need to know set numbers in advance.
func RecomputeAll(ctx context.Context, st *store.Store) ([]SetResult, error) {
	sets, err := st.DistinctSetNumbers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list set numbers: %w", err)
	}

	results := make([]SetResult, 0, len(sets))
	for _, set := range sets {
		r, err := RecomputeSet(ctx, st, set)
		if err != nil {
			return results, err
		}
		results = append(results, r)
	}
	return results, nil
}

// RecomputeSet rebuilds unit/trait/augment meta stats for a single TFT set
// number, in one pass each via SQL (see internal/store/aggregates.go).
func RecomputeSet(ctx context.Context, st *store.Store, setNumber int) (SetResult, error) {
	runID, err := st.StartMetaComputeRun(ctx, setNumber)
	if err != nil {
		return SetResult{}, fmt.Errorf("start meta compute run: %w", err)
	}

	units, err := st.RecomputeUnitStats(ctx, setNumber)
	if err != nil {
		st.FinishMetaComputeRun(ctx, runID, "failed", 0)
		return SetResult{}, fmt.Errorf("recompute unit stats: %w", err)
	}
	traits, err := st.RecomputeTraitStats(ctx, setNumber)
	if err != nil {
		st.FinishMetaComputeRun(ctx, runID, "failed", 0)
		return SetResult{}, fmt.Errorf("recompute trait stats: %w", err)
	}
	augments, err := st.RecomputeAugmentStats(ctx, setNumber)
	if err != nil {
		st.FinishMetaComputeRun(ctx, runID, "failed", 0)
		return SetResult{}, fmt.Errorf("recompute augment stats: %w", err)
	}

	if err := st.FinishMetaComputeRun(ctx, runID, "completed", units+traits+augments); err != nil {
		return SetResult{}, fmt.Errorf("finish meta compute run: %w", err)
	}

	return SetResult{
		TFTSetNumber:    setNumber,
		UnitsWritten:    units,
		TraitsWritten:   traits,
		AugmentsWritten: augments,
	}, nil
}
