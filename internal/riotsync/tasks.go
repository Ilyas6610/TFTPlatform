package riotsync

import (
	"context"
	"fmt"

	"tft-platform/internal/aggregate"
	"tft-platform/internal/ingest"
	"tft-platform/internal/riotapi"
	"tft-platform/internal/store"
)

// BuildTasks wires the ingest and aggregate packages into scheduled tasks.
// riot must be the process's single shared client.
func BuildTasks(cfg Config, riot *riotapi.Client, st *store.Store) []Task {
	var tasks []Task

	for _, p := range cfg.Platforms {
		p := p
		tasks = append(tasks, Task{
			Name:     "seed_leaderboard:" + string(p),
			Interval: cfg.SeedInterval,
			Run: func(ctx context.Context) Outcome {
				r, err := ingest.SeedLeaderboard(ctx, riot, st, p)
				return Outcome{Requests: r.RequestsMade, Items: r.PlayersSeeded, Stopped: r.Stopped, Err: err}
			},
		})
	}

	tasks = append(tasks, Task{
		Name:     "crawl_queue",
		Interval: cfg.CrawlInterval,
		Run: func(ctx context.Context) Outcome {
			r, err := ingest.CrawlQueue(ctx, riot, st, cfg.CrawlPUUIDs, cfg.CrawlIDsPerPUUID, cfg.CrawlRequests)
			return Outcome{Requests: r.RequestsMade, Items: r.MatchesIngested, Stopped: r.Stopped, Err: err}
		},
	})

	for _, p := range cfg.Platforms {
		p := p
		tasks = append(tasks, Task{
			Name:     "resolve_names:" + string(p),
			Interval: cfg.NamesInterval,
			Run: func(ctx context.Context) Outcome {
				puuids, err := st.UnresolvedLeaderboardPUUIDs(ctx, string(p), cfg.NamesBatch)
				if err != nil || len(puuids) == 0 {
					return Outcome{Err: err}
				}
				r, err := ingest.ResolveNames(ctx, riot, st, p, puuids)
				return Outcome{Requests: r.RequestsMade, Items: r.Resolved, Stopped: r.Stopped, Err: err}
			},
		})
	}

	// No Riot calls; recomputes from what the crawl stored.
	tasks = append(tasks, Task{
		Name:     "aggregate",
		Interval: cfg.AggregateInterval,
		Run: func(ctx context.Context) Outcome {
			results, err := aggregate.RecomputeAll(ctx, st)
			if err != nil {
				return Outcome{Err: fmt.Errorf("aggregate: %w", err)}
			}
			return Outcome{Items: len(results)}
		},
	})
	return tasks
}

// runTypePrefix marks ingest_runs rows as riotsync's (ingestcli writes to the
// same table), so CloseStale can't touch a run that belongs to someone else.
const runTypePrefix = "riotsync:"

// StoreRecorder records runs in ingest_runs, the same table ingestcli uses.
type StoreRecorder struct{ Store *store.Store }

func (r StoreRecorder) Start(ctx context.Context, name string) (int64, error) {
	return r.Store.StartIngestRun(ctx, runTypePrefix+name)
}

// CloseStale marks riotsync runs still "running" as interrupted (the same
// status a graceful shutdown records). Call it at
// startup while holding the singleton lock: any such row was left by an
// earlier instance that died (kill -9, OOM, power loss) mid-run. It returns
// how many rows it closed.
func (r StoreRecorder) CloseStale(ctx context.Context) (int64, error) {
	tag, err := r.Store.Pool.Exec(ctx, `
		UPDATE ingest_runs
		SET finished_at = now(), status = 'interrupted',
		    error_detail = 'riotsync stopped before this run finished'
		WHERE finished_at IS NULL AND run_type LIKE $1
	`, runTypePrefix+"%")
	return tag.RowsAffected(), err
}

func (r StoreRecorder) Finish(ctx context.Context, id int64, status string, o Outcome) error {
	detail := ""
	if o.Err != nil {
		detail = o.Err.Error()
	}
	return r.Store.FinishIngestRun(ctx, id, status, o.Requests, o.Items, detail)
}

// lockKey is the Postgres advisory lock that keeps a single riotsync running
// per database ("riotsync" as bytes).
const lockKey int64 = 0x72696f7473796e63

// AcquireSingleton takes the advisory lock on a dedicated connection. It
// returns false if another instance holds it. Call release on shutdown; the
// lock is also dropped automatically if the process or connection dies.
func AcquireSingleton(ctx context.Context, st *store.Store) (ok bool, release func(), err error) {
	conn, err := st.Pool.Acquire(ctx)
	if err != nil {
		return false, nil, err
	}
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, lockKey).Scan(&ok); err != nil || !ok {
		conn.Release()
		return false, nil, err
	}
	return true, func() {
		conn.Exec(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock($1)`, lockKey)
		conn.Release()
	}, nil
}
