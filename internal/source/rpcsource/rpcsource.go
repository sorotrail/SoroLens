// Package rpcsource implements source.EventSource for standalone mode, where
// SoroLens polls Stellar RPC itself and serves events back out of its own
// Postgres database.
//
// The read path here is deliberately thin: it forwards to the store, which
// does the real work. The write path — the poller that fills that store — is
// in internal/ingest.
package rpcsource

import (
	"context"
	"fmt"
	"sync"

	"github.com/sorotrail/sorolens/internal/rpc"
	"github.com/sorotrail/sorolens/internal/source"
	"github.com/sorotrail/sorolens/internal/store"
)

// RetentionNote explains standalone mode's central limitation. Stellar RPC
// keeps contract events for roughly 24 hours to 7 days, so an instance can
// only ever capture what was emitted while it was running. The UI shows this
// so nobody mistakes an empty explorer for a broken one.
const RetentionNote = "Standalone mode captures events only while SoroLens is running: " +
	"Stellar RPC retains contract events for about 24 hours to 7 days, so history " +
	"from before this instance started is not available. Point SOURCE_MODE at a " +
	"SoroTrail indexer to browse deeper history."

// Source reads stored events. It satisfies source.EventSource.
type Source struct {
	store store.Store
	rpc   rpc.Client
}

// New returns a standalone-mode source. rpcClient is used only for status
// reporting; all reads come from the store.
func New(st store.Store, rpcClient rpc.Client) *Source {
	return &Source{store: st, rpc: rpcClient}
}

// ListContracts returns contracts ordered by most recent activity.
func (s *Source) ListContracts(ctx context.Context, q source.ContractQuery) (source.ContractPage, error) {
	contracts, next, err := s.store.ListContracts(ctx, q)
	if err != nil {
		return source.ContractPage{}, err
	}
	return source.ContractPage{Contracts: contracts, NextCursor: next}, nil
}

// ListEvents returns events newest first.
func (s *Source) ListEvents(ctx context.Context, q source.EventQuery) (source.EventPage, error) {
	events, next, err := s.store.QueryEvents(ctx, q)
	if err != nil {
		return source.EventPage{}, err
	}
	return source.EventPage{Events: events, NextCursor: next}, nil
}

// GetEvent returns one event by TOID.
func (s *Source) GetEvent(ctx context.Context, id string) (source.Event, error) {
	return s.store.GetEvent(ctx, id)
}

// ContractStats summarizes one contract's stored events.
func (s *Source) ContractStats(ctx context.Context, contractID string) (source.ContractStats, error) {
	return s.store.ContractStats(ctx, contractID)
}

// Stats summarizes everything stored. Standalone counts are exact.
func (s *Source) Stats(ctx context.Context) (source.Stats, error) {
	return s.store.Stats(ctx)
}

// Status reports database and RPC health concurrently under a shared deadline.
// It never returns an error: an unreachable dependency becomes an unhealthy
// Status so the UI can always render its banner, while Dependencies lets
// monitoring distinguish a fatal database outage ("down") from a stalled RPC
// where stored events are still queryable ("degraded").
func (s *Source) Status(ctx context.Context) source.Status {
	probeCtx, cancel := context.WithTimeout(ctx, source.ProbeTimeout)
	defer cancel()

	var (
		wg     sync.WaitGroup
		dbErr  error
		health rpc.Health
		rpcErr error
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		dbErr = s.store.Ping(probeCtx)
	}()
	go func() {
		defer wg.Done()
		health, rpcErr = s.rpc.GetHealth(probeCtx)
	}()
	wg.Wait()

	dbDep := source.DependencyStatus{Healthy: true, Status: source.StateOK}
	if dbErr != nil {
		dbDep = source.DependencyStatus{
			Healthy: false,
			Status:  source.StateDown,
			Detail:  source.SanitizeDetail(dbErr.Error()),
		}
	}

	rpcDep := source.DependencyStatus{Healthy: true, Status: source.StateOK}
	switch {
	case rpcErr != nil:
		rpcDep = source.DependencyStatus{
			Healthy: false,
			Status:  source.StateDown,
			Detail:  source.SanitizeDetail(rpcErr.Error()),
		}
	case health.Status != "healthy":
		rpcDep = source.DependencyStatus{
			Healthy: false,
			Status:  source.StateDegraded,
			Detail:  fmt.Sprintf("stellar rpc reports %q", source.SanitizeDetail(health.Status)),
		}
	}

	st := source.Status{
		Mode:          string(sourceModeRPC),
		Healthy:       dbDep.Healthy && rpcDep.Healthy,
		State:         source.StateOK,
		RetentionNote: RetentionNote,
		Dependencies: map[string]source.DependencyStatus{
			"database": dbDep,
			"rpc":      rpcDep,
		},
	}
	if rpcDep.Healthy {
		st.LatestLedger = int64(health.LatestLedger)
	}

	switch {
	case !dbDep.Healthy && !rpcDep.Healthy:
		st.State = source.StateDown
		if rpcErr != nil {
			st.Detail = fmt.Sprintf("database unreachable: %s; stellar rpc unreachable: %s", dbDep.Detail, rpcDep.Detail)
		} else {
			st.Detail = fmt.Sprintf("database unreachable: %s; %s", dbDep.Detail, rpcDep.Detail)
		}
	case !dbDep.Healthy:
		st.State = source.StateDown
		st.Detail = fmt.Sprintf("database unreachable: %s", dbDep.Detail)
	case !rpcDep.Healthy:
		// Stored events remain queryable while Postgres is up, so an RPC
		// failure degrades ingestion rather than taking the explorer down.
		st.State = source.StateDegraded
		if rpcErr != nil {
			st.Detail = fmt.Sprintf("stellar rpc unreachable: %s", rpcDep.Detail)
		} else {
			st.Detail = rpcDep.Detail
		}
	}
	return st
}

// sourceModeRPC mirrors config.ModeRPC. It is duplicated rather than imported
// to keep this package free of a dependency on config.
const sourceModeRPC = "rpc"
