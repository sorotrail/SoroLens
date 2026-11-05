// Package ingest polls Stellar RPC for contract events and writes them to the
// store. It runs only in standalone mode.
package ingest

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/sorotrail/sorolens/internal/decode"
	"github.com/sorotrail/sorolens/internal/metrics"
	"github.com/sorotrail/sorolens/internal/rpc"
	"github.com/sorotrail/sorolens/internal/source"
	"github.com/sorotrail/sorolens/internal/store"
)

// MaxPagesPerBatch bounds how many cursor pages one batch may follow in a
// single poll. Without it, a node that keeps returning full pages could hold
// the loop indefinitely and starve the shutdown path.
const MaxPagesPerBatch = 1000

// Modes for NewContractStart, deciding where a contract added to
// WATCHED_CONTRACTS after initial deployment begins ingesting.
const (
	NewContractStartRetention = "retention"
	NewContractStartTip       = "tip"
)

// Options configure an Ingester.
type Options struct {
	// WatchedContracts limits ingestion to these contract IDs. Empty ingests
	// every contract event using the single-row global cursor in ingest_state.
	WatchedContracts []string
	// PollInterval is how long to sleep once caught up to the chain tip.
	PollInterval time.Duration
	// StartLedger forces a cold start from this ledger. Zero reaches back
	// RetentionLedgers from the tip instead.
	StartLedger uint32
	// RetentionLedgers is the cold-start reach-back when StartLedger is zero.
	RetentionLedgers uint32
	// NewContractStart decides where a newly added watched contract starts
	// ingesting from: "retention" (default: reach back RetentionLedgers or
	// StartLedger), "tip" (start from the current chain tip), or a decimal
	// ledger number.
	NewContractStart string
}

// Ingester polls the RPC and persists what it finds.
type Ingester struct {
	rpc     rpc.Client
	store   store.Store
	decoder decode.Decoder
	opts    Options
	log     *slog.Logger
	// metrics is optional instrumentation; nil-safe, see internal/metrics.
	metrics *metrics.Metrics
}

// New builds an Ingester.
func New(client rpc.Client, st store.Store, dec decode.Decoder, opts Options, log *slog.Logger) *Ingester {
	if opts.PollInterval <= 0 {
		opts.PollInterval = 5 * time.Second
	}
	return &Ingester{rpc: client, store: st, decoder: dec, opts: opts, log: log}
}

// WithMetrics attaches Prometheus instrumentation to the ingest loop.
func (i *Ingester) WithMetrics(m *metrics.Metrics) *Ingester {
	i.metrics = m
	return i
}

// Run polls until ctx is cancelled. A failed poll is logged and retried on the
// next tick rather than stopping ingestion, since RPC endpoints routinely
// return transient errors.
func (i *Ingester) Run(ctx context.Context) error {
	i.log.Info("ingester starting",
		"watched_contracts", len(i.opts.WatchedContracts),
		"poll_interval", i.opts.PollInterval)

	if len(i.opts.WatchedContracts) > rpc.MaxWatchedContracts {
		i.log.Warn("watching many contracts requires several requests per poll",
			"count", len(i.opts.WatchedContracts),
			"per_request", rpc.MaxWatchedContracts)
	}

	timer := time.NewTimer(0)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			i.log.Info("ingester stopping")
			return nil
		case <-timer.C:
		}

		start := time.Now()
		err := i.pollOnce(ctx)
		i.metrics.RecordPoll(err == nil, time.Since(start))
		if err != nil {
			if ctx.Err() != nil {
				i.log.Info("ingester stopping")
				return nil
			}
			i.log.Error("poll failed", "error", err)
		}
		timer.Reset(i.opts.PollInterval)
	}
}

// pollOnce ingests everything from the resume point up to the current tip.
//
// When WATCHED_CONTRACTS is empty, SoroLens ingests all contract events on the
// network and tracks a single global cursor in ingest_state (id = 1). When
// WATCHED_CONTRACTS is non-empty, each contract tracks its own cursor in
// contract_ingest_state so newly added contracts can backfill independently and
// a failure on one contract never advances its cursor or blocks the others.
func (i *Ingester) pollOnce(ctx context.Context) error {
	latest, err := i.rpc.GetLatestLedger(ctx)
	if err != nil {
		return fmt.Errorf("fetching latest ledger: %w", err)
	}

	if len(i.opts.WatchedContracts) == 0 {
		return i.pollUnwatched(ctx, latest.Sequence)
	}
	return i.pollWatched(ctx, latest.Sequence)
}

// pollUnwatched runs the global ingest-everything mode when WATCHED_CONTRACTS
// is empty.
func (i *Ingester) pollUnwatched(ctx context.Context, latest uint32) error {
	start, err := i.resumeGlobalLedger(ctx, latest)
	if err != nil {
		return err
	}
	if start > latest {
		// Already caught up; nothing has closed since the last poll.
		i.metrics.SetLag(0)
		return nil
	}
	i.metrics.SetLag(int64(latest) - int64(start))

	// endLedger is exclusive, so +1 includes the tip itself.
	endExclusive := latest + 1

	var total int64
	for _, batch := range filterBatches(nil) {
		n, _, err := i.ingestBatch(ctx, batch, start, endExclusive)
		total += n
		if err != nil {
			return err
		}
	}

	if err := i.store.SaveIngestState(ctx, store.IngestState{LastLedger: int64(latest)}); err != nil {
		return err
	}

	i.metrics.RecordEvents(total)
	i.metrics.SetLag(0)

	if total > 0 {
		i.log.Info("ingested events", "count", total, "from_ledger", start, "to_ledger", latest)
	} else {
		i.log.Debug("no new events", "from_ledger", start, "to_ledger", latest)
	}
	return nil
}

// pollWatched tracks per-contract cursors for each contract in
// WatchedContracts, grouping contracts that share a resume ledger into shared
// windowed RPC reads while isolating per-contract failures.
func (i *Ingester) pollWatched(ctx context.Context, latest uint32) error {
	contractStates, err := i.store.GetContractIngestStates(ctx)
	if err != nil {
		return err
	}
	if contractStates == nil {
		contractStates = make(map[string]store.ContractIngestState)
	}

	globalState, err := i.store.GetIngestState(ctx)
	if err != nil {
		return err
	}

	// Seamless upgrade from a pre-existing deployment: if no per-contract
	// cursors exist yet but the global cursor in ingest_state has already
	// advanced, seed every currently watched contract from the global cursor
	// so upgrading does not re-read the retention window.
	if len(contractStates) == 0 && globalState.LastLedger > 0 {
		for _, cid := range i.opts.WatchedContracts {
			seeded := store.ContractIngestState{
				ContractID: cid,
				LastLedger: globalState.LastLedger,
				LastCursor: globalState.LastCursor,
			}
			if err := i.store.SaveContractIngestState(ctx, seeded); err != nil {
				return err
			}
			contractStates[cid] = seeded
		}
	}

	hasExistingCursors := len(contractStates) > 0

	var (
		startOrder []uint32
		byStart    = make(map[uint32][]string)
		maxLag     int64
	)

	for _, cid := range i.opts.WatchedContracts {
		var start uint32
		if st, ok := contractStates[cid]; ok && st.LastLedger > 0 {
			start = uint32(st.LastLedger) + 1
		} else if hasExistingCursors || strings.TrimSpace(i.opts.NewContractStart) != "" {
			start = i.newContractStartLedger(latest)
		} else {
			start = i.coldStartLedger(latest)
		}

		if start > latest {
			i.metrics.SetContractLag(cid, 0)
			continue
		}

		lag := int64(latest) - int64(start)
		i.metrics.SetContractLag(cid, lag)
		if lag > maxLag {
			maxLag = lag
		}

		if _, exists := byStart[start]; !exists {
			startOrder = append(startOrder, start)
		}
		byStart[start] = append(byStart[start], cid)
	}

	if len(startOrder) == 0 {
		i.metrics.SetLag(0)
		return nil
	}
	i.metrics.SetLag(maxLag)

	endExclusive := latest + 1
	var (
		total    int64
		pollErrs []error
		minFrom  uint32
	)

	for _, start := range startOrder {
		if minFrom == 0 || start < minFrom {
			minFrom = start
		}
		contracts := byStart[start]
		for _, batch := range filterBatches(contracts) {
			batchContracts := contractsInFilters(batch)
			n, failedContracts, batchErr := i.ingestBatch(ctx, batch, start, endExclusive)
			total += n

			if batchErr != nil && len(batchContracts) > 1 && len(failedContracts) == 0 {
				// A transport or RPC error hit a multi-contract request. Retry
				// each contract in the batch individually so one bad contract
				// cannot block the rest.
				for _, cid := range batchContracts {
					singleFilter := []rpc.EventFilter{{Type: "contract", ContractIDs: []string{cid}}}
					sn, sFailed, sErr := i.ingestBatch(ctx, singleFilter, start, endExclusive)
					total += sn
					if sErr != nil || sFailed[cid] != nil {
						if sErr != nil {
							pollErrs = append(pollErrs, fmt.Errorf("contract %s: %w", cid, sErr))
						} else {
							pollErrs = append(pollErrs, fmt.Errorf("contract %s: %w", cid, sFailed[cid]))
						}
						continue
					}
					if err := i.advanceContract(ctx, cid, latest, contractStates); err != nil {
						pollErrs = append(pollErrs, err)
					}
				}
				continue
			}

			if batchErr != nil && len(failedContracts) == 0 {
				pollErrs = append(pollErrs, batchErr)
				continue
			}

			for _, cid := range batchContracts {
				if cErr, failed := failedContracts[cid]; failed {
					pollErrs = append(pollErrs, fmt.Errorf("contract %s: %w", cid, cErr))
					continue
				}
				if err := i.advanceContract(ctx, cid, latest, contractStates); err != nil {
					pollErrs = append(pollErrs, err)
				}
			}
		}
	}

	i.metrics.RecordEvents(total)

	// Keep the single-row ingest_state at the conservative floor across all
	// watched contracts so observability and fallback consumers see the oldest
	// un-ingested point.
	floorLedger := int64(latest)
	var remainingMaxLag int64
	for _, cid := range i.opts.WatchedContracts {
		st, ok := contractStates[cid]
		if !ok || st.LastLedger < floorLedger {
			floorLedger = st.LastLedger
		}
		if st.LastLedger > 0 && uint32(st.LastLedger) < latest {
			lag := int64(latest) - st.LastLedger
			if lag > remainingMaxLag {
				remainingMaxLag = lag
			}
		}
	}
	if floorLedger > 0 && floorLedger > globalState.LastLedger {
		if err := i.store.SaveIngestState(ctx, store.IngestState{LastLedger: floorLedger}); err != nil {
			pollErrs = append(pollErrs, err)
		}
	}

	if len(pollErrs) > 0 {
		i.metrics.SetLag(remainingMaxLag)
		return errors.Join(pollErrs...)
	}

	i.metrics.SetLag(0)
	if total > 0 {
		i.log.Info("ingested events", "count", total, "from_ledger", minFrom, "to_ledger", latest)
	} else {
		i.log.Debug("no new events", "from_ledger", minFrom, "to_ledger", latest)
	}
	return nil
}

func (i *Ingester) advanceContract(ctx context.Context, contractID string, latest uint32, states map[string]store.ContractIngestState) error {
	next := store.ContractIngestState{
		ContractID: contractID,
		LastLedger: int64(latest),
	}
	if err := i.store.SaveContractIngestState(ctx, next); err != nil {
		return err
	}
	states[contractID] = next
	i.metrics.SetContractLag(contractID, 0)
	return nil
}

// ingestBatch follows the cursor for one filter batch until the stream drains.
// When Watching specific contracts, a decode failure on one contract's event is
// recorded in failedContracts so healthy contracts in the same windowed read
// still store their events and advance their cursors.
func (i *Ingester) ingestBatch(ctx context.Context, filters []rpc.EventFilter, start, endExclusive uint32) (int64, map[string]error, error) {
	var (
		cursor          string
		inserted        int64
		failedContracts = make(map[string]error)
		perContractMode = len(contractsInFilters(filters)) > 0
	)

	for page := 0; page < MaxPagesPerBatch; page++ {
		if err := ctx.Err(); err != nil {
			return inserted, nil, err
		}

		req := rpc.GetEventsRequest{
			Filters:    filters,
			Pagination: &rpc.Pagination{Limit: rpc.DefaultEventsLimit},
		}
		// startLedger and a cursor are mutually exclusive: the cursor already
		// encodes the position, and nodes reject requests carrying both.
		if cursor == "" {
			req.StartLedger = start
			req.EndLedger = endExclusive
		} else {
			req.Pagination.Cursor = cursor
		}

		res, err := i.rpc.GetEvents(ctx, req)
		if err != nil {
			return inserted, nil, fmt.Errorf("fetching events: %w", err)
		}
		if len(res.Events) == 0 {
			return inserted, failedContracts, nil
		}

		events, decodeFailures, err := i.convertIsolated(res.Events, perContractMode, failedContracts)
		if err != nil {
			return inserted, nil, err
		}
		for cid, cErr := range decodeFailures {
			failedContracts[cid] = cErr
		}

		if len(events) > 0 {
			n, err := i.store.UpsertEvents(ctx, events)
			if err != nil {
				return inserted, nil, err
			}
			inserted += n
		}

		// A short page means the stream is drained for now.
		if len(res.Events) < rpc.DefaultEventsLimit {
			return inserted, failedContracts, nil
		}
		next := nextCursor(res)
		if next == "" || next == cursor {
			return inserted, failedContracts, nil
		}
		cursor = next
	}

	i.log.Warn("stopped following cursor at the page cap; will resume next poll",
		"max_pages", MaxPagesPerBatch)
	return inserted, failedContracts, nil
}

// convertIsolated decodes RPC events into the storage shape. When isolate is
// true, an event that fails to decode marks its contract as failed in this poll
// cycle and skips any further events from that contract so other contracts in
// the same RPC window are unaffected.
func (i *Ingester) convertIsolated(events []rpc.Event, isolate bool, alreadyFailed map[string]error) ([]source.Event, map[string]error, error) {
	out := make([]source.Event, 0, len(events))
	failed := make(map[string]error)
	for _, e := range events {
		if isolate && e.ContractID != "" {
			if _, bad := alreadyFailed[e.ContractID]; bad {
				continue
			}
			if _, bad := failed[e.ContractID]; bad {
				continue
			}
		}
		topics, value, err := decode.TopicsValue(i.decoder, decode.RawEvent{
			Topic:     e.Topic,
			Value:     e.Value,
			TopicJSON: e.TopicJSON,
			ValueJSON: e.ValueJSON,
		})
		if err != nil {
			decErr := fmt.Errorf("decoding event %s: %w", e.ID, err)
			if !isolate || e.ContractID == "" {
				return nil, nil, decErr
			}
			failed[e.ContractID] = decErr
			out = removeContractEvents(out, e.ContractID)
			continue
		}
		out = append(out, source.Event{
			ID:               e.ID,
			ContractID:       e.ContractID,
			Ledger:           int64(e.Ledger),
			Type:             e.Type,
			TxHash:           e.TxHash,
			TxIndex:          e.TxIndex,
			OpIndex:          e.OpIndex,
			InSuccessfulCall: e.InSuccessfulContractCall,
			Topics:           topics,
			Value:            value,
			LedgerClosedAt:   e.LedgerClosedAt,
		})
	}
	return out, failed, nil
}

func removeContractEvents(events []source.Event, contractID string) []source.Event {
	out := events[:0]
	for _, e := range events {
		if e.ContractID != contractID {
			out = append(out, e)
		}
	}
	return out
}

func contractsInFilters(filters []rpc.EventFilter) []string {
	var out []string
	for _, f := range filters {
		out = append(out, f.ContractIDs...)
	}
	return out
}

// resumeGlobalLedger decides where an unwatched poll should start: just past
// whatever was ingested last, or a cold-start position when nothing has been
// ingested yet.
func (i *Ingester) resumeGlobalLedger(ctx context.Context, latest uint32) (uint32, error) {
	state, err := i.store.GetIngestState(ctx)
	if err != nil {
		return 0, err
	}
	if state.LastLedger > 0 {
		return uint32(state.LastLedger) + 1, nil
	}
	return i.coldStartLedger(latest), nil
}

func (i *Ingester) coldStartLedger(latest uint32) uint32 {
	if i.opts.StartLedger > 0 {
		return i.opts.StartLedger
	}
	// Cold start: reach back over the retention window, clamped at ledger 1.
	if latest > i.opts.RetentionLedgers {
		return latest - i.opts.RetentionLedgers
	}
	return 1
}

// newContractStartLedger resolves the starting ledger for a newly added watched
// contract according to Options.NewContractStart ("retention", "tip", or an
// explicit ledger number).
func (i *Ingester) newContractStartLedger(latest uint32) uint32 {
	mode := strings.ToLower(strings.TrimSpace(i.opts.NewContractStart))
	switch mode {
	case "", NewContractStartRetention:
		return i.coldStartLedger(latest)
	case NewContractStartTip, "latest":
		if latest == 0 {
			return 1
		}
		return latest
	default:
		if n, err := strconv.ParseUint(mode, 10, 32); err == nil && n > 0 {
			return uint32(n)
		}
		return i.coldStartLedger(latest)
	}
}

// nextCursor prefers the response's own cursor and falls back to the last
// event's paging token, which is what older nodes provide.
func nextCursor(res rpc.GetEventsResult) string {
	if res.Cursor != "" {
		return res.Cursor
	}
	if len(res.Events) > 0 {
		return res.Events[len(res.Events)-1].PagingToken
	}
	return ""
}

// filterBatches splits watched contracts into request-sized groups, honoring
// both the per-filter contract cap and the per-request filter cap. With no
// watched contracts it returns a single batch matching every contract event.
func filterBatches(contracts []string) [][]rpc.EventFilter {
	if len(contracts) == 0 {
		return [][]rpc.EventFilter{{{Type: "contract"}}}
	}

	var filters []rpc.EventFilter
	for start := 0; start < len(contracts); start += rpc.MaxContractIDsPerFilter {
		end := min(start+rpc.MaxContractIDsPerFilter, len(contracts))
		filters = append(filters, rpc.EventFilter{
			Type:        "contract",
			ContractIDs: contracts[start:end],
		})
	}

	var batches [][]rpc.EventFilter
	for start := 0; start < len(filters); start += rpc.MaxFiltersPerRequest {
		end := min(start+rpc.MaxFiltersPerRequest, len(filters))
		batches = append(batches, filters[start:end])
	}
	return batches
}

// ErrStopped signals a clean shutdown to callers that distinguish it from a
// genuine failure.
var ErrStopped = errors.New("ingester stopped")
