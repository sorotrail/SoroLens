package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sorotrail/sorolens/internal/rpc"
	"github.com/sorotrail/sorolens/internal/store"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTestIngester(client rpc.Client, st store.Store, opts Options) *Ingester {
	return New(client, st, passthroughDecoder{}, opts, testLogger())
}

// event builds an RPC event with a decodable payload.
func event(id string, ledger uint32, contract string) rpc.Event {
	return rpc.Event{
		ID:                       id,
		Type:                     "contract",
		Ledger:                   ledger,
		ContractID:               contract,
		PagingToken:              id,
		InSuccessfulContractCall: true,
		TxHash:                   "hash-" + id,
		Topic:                    []string{"transfer"},
		Value:                    "amount",
		LedgerClosedAt:           time.Unix(1700000000, 0).UTC(),
	}
}

func TestPollOnceStoresDecodedEvents(t *testing.T) {
	client := &mockRPC{
		latest: 100,
		responses: []rpc.GetEventsResult{
			{Events: []rpc.Event{event("0000000100-0000000001", 100, "CABC")}, LatestLedger: 100},
		},
	}
	st := newMockStore()

	ing := newTestIngester(client, st, Options{RetentionLedgers: 50})
	require.NoError(t, ing.pollOnce(context.Background()))

	require.Equal(t, 1, st.storedCount())

	stored, ok := st.stored("0000000100-0000000001")
	require.True(t, ok)
	assert.Equal(t, "CABC", stored.ContractID)
	assert.Equal(t, int64(100), stored.Ledger)
	assert.Equal(t, "contract", stored.Type)
	assert.Equal(t, "hash-0000000100-0000000001", stored.TxHash)
	assert.True(t, stored.InSuccessfulCall)
	// The decoder ran over the base64 fields, since the node sent no JSON.
	assert.JSONEq(t, `[{"symbol":"transfer"}]`, string(stored.Topics))
	assert.JSONEq(t, `{"symbol":"amount"}`, string(stored.Value))

	// Progress is recorded so a restart resumes rather than re-reading.
	assert.Equal(t, int64(100), st.savedState().LastLedger)
}

func TestPollOnceColdStartUsesRetentionWindow(t *testing.T) {
	client := &mockRPC{latest: 10000}
	st := newMockStore()

	ing := newTestIngester(client, st, Options{RetentionLedgers: 1000})
	require.NoError(t, ing.pollOnce(context.Background()))

	requests := client.capturedRequests()
	require.NotEmpty(t, requests)
	assert.Equal(t, uint32(9000), requests[0].StartLedger, "expected latest minus the retention window")
	assert.Equal(t, uint32(10001), requests[0].EndLedger, "endLedger is exclusive, so it must exceed the tip")
}

func TestPollOnceColdStartHonorsStartLedger(t *testing.T) {
	client := &mockRPC{latest: 10000}
	st := newMockStore()

	ing := newTestIngester(client, st, Options{StartLedger: 42, RetentionLedgers: 1000})
	require.NoError(t, ing.pollOnce(context.Background()))

	requests := client.capturedRequests()
	require.NotEmpty(t, requests)
	assert.Equal(t, uint32(42), requests[0].StartLedger)
}

func TestPollOnceColdStartClampsAtGenesis(t *testing.T) {
	// A retention window wider than the chain must not underflow past ledger 1.
	client := &mockRPC{latest: 50}
	st := newMockStore()

	ing := newTestIngester(client, st, Options{RetentionLedgers: 1000})
	require.NoError(t, ing.pollOnce(context.Background()))

	requests := client.capturedRequests()
	require.NotEmpty(t, requests)
	assert.Equal(t, uint32(1), requests[0].StartLedger)
}

func TestPollOnceResumesFromSavedState(t *testing.T) {
	client := &mockRPC{latest: 500}
	st := newMockStore()
	st.state = store.IngestState{LastLedger: 200}

	ing := newTestIngester(client, st, Options{RetentionLedgers: 1000})
	require.NoError(t, ing.pollOnce(context.Background()))

	requests := client.capturedRequests()
	require.NotEmpty(t, requests)
	assert.Equal(t, uint32(201), requests[0].StartLedger, "expected to resume just past the last ingested ledger")
}

func TestPollOnceSkipsWhenCaughtUp(t *testing.T) {
	client := &mockRPC{latest: 200}
	st := newMockStore()
	st.state = store.IngestState{LastLedger: 200}

	ing := newTestIngester(client, st, Options{RetentionLedgers: 1000})
	require.NoError(t, ing.pollOnce(context.Background()))

	assert.Empty(t, client.capturedRequests(), "expected no fetch when already at the tip")
}

func TestIngestBatchFollowsCursor(t *testing.T) {
	// A full page means more may remain, so the ingester follows the cursor;
	// the short page that follows ends the stream.
	full := make([]rpc.Event, rpc.DefaultEventsLimit)
	for i := range full {
		full[i] = event(fmt.Sprintf("page1-%04d", i), 100, "CABC")
	}

	client := &mockRPC{
		latest: 100,
		responses: []rpc.GetEventsResult{
			{Events: full, LatestLedger: 100, Cursor: "cursor-1"},
			{Events: []rpc.Event{event("page2-0001", 100, "CABC")}, LatestLedger: 100},
		},
	}
	st := newMockStore()

	ing := newTestIngester(client, st, Options{RetentionLedgers: 50})
	require.NoError(t, ing.pollOnce(context.Background()))

	assert.Equal(t, rpc.DefaultEventsLimit+1, st.storedCount())

	requests := client.capturedRequests()
	require.Len(t, requests, 2)
	// The first request opens the ledger range; the second continues by cursor
	// and must not also carry startLedger, which nodes reject.
	assert.Equal(t, uint32(50), requests[0].StartLedger, "cold start reaches back by the retention window")
	assert.Equal(t, "cursor-1", requests[1].Pagination.Cursor)
	assert.Zero(t, requests[1].StartLedger, "startLedger and cursor are mutually exclusive")
}

func TestIngestBatchStopsOnShortPage(t *testing.T) {
	client := &mockRPC{
		latest: 100,
		responses: []rpc.GetEventsResult{
			{Events: []rpc.Event{event("only-1", 100, "CABC")}, LatestLedger: 100, Cursor: "cursor-1"},
		},
	}
	st := newMockStore()

	ing := newTestIngester(client, st, Options{RetentionLedgers: 50})
	require.NoError(t, ing.pollOnce(context.Background()))

	assert.Len(t, client.capturedRequests(), 1, "a short page means the stream is drained")
}

func TestUpsertIsIdempotent(t *testing.T) {
	// Re-reading a ledger range after a restart must not duplicate rows.
	makeResponse := func() rpc.GetEventsResult {
		return rpc.GetEventsResult{
			Events:       []rpc.Event{event("0000000100-0000000001", 100, "CABC")},
			LatestLedger: 100,
		}
	}

	client := &mockRPC{latest: 100, responses: []rpc.GetEventsResult{makeResponse(), makeResponse()}}
	st := newMockStore()

	ing := newTestIngester(client, st, Options{RetentionLedgers: 50})
	require.NoError(t, ing.pollOnce(context.Background()))

	// Rewind so the second poll covers the same range again.
	st.state = store.IngestState{}
	require.NoError(t, ing.pollOnce(context.Background()))

	assert.Equal(t, 1, st.storedCount(), "expected the duplicate to be ignored")
}

func TestPollOncePrefersNodeDecodedJSON(t *testing.T) {
	// When the node sends decoded JSON there is nothing to decode locally, so
	// the values must pass through untouched rather than being re-wrapped.
	client := &mockRPC{
		latest: 100,
		responses: []rpc.GetEventsResult{{
			Events: []rpc.Event{{
				ID:         "0000000100-0000000001",
				Type:       "contract",
				Ledger:     100,
				ContractID: "CABC",
				TopicJSON:  []json.RawMessage{json.RawMessage(`{"symbol":"mint"}`)},
				ValueJSON:  json.RawMessage(`{"u32":5}`),
			}},
			LatestLedger: 100,
		}},
	}
	st := newMockStore()

	ing := newTestIngester(client, st, Options{RetentionLedgers: 50})
	require.NoError(t, ing.pollOnce(context.Background()))

	stored, ok := st.stored("0000000100-0000000001")
	require.True(t, ok)
	assert.JSONEq(t, `[{"symbol":"mint"}]`, string(stored.Topics))
	assert.JSONEq(t, `{"u32":5}`, string(stored.Value))
}

func TestPollOncePropagatesFetchErrors(t *testing.T) {
	client := &mockRPC{latest: 100, errs: []error{errors.New("node exploded")}}
	st := newMockStore()

	ing := newTestIngester(client, st, Options{RetentionLedgers: 50})
	err := ing.pollOnce(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "node exploded")
	assert.Zero(t, st.savedState().LastLedger, "progress must not advance past a failure")
}

func TestPollOnceDoesNotAdvanceOnStoreFailure(t *testing.T) {
	client := &mockRPC{
		latest:    100,
		responses: []rpc.GetEventsResult{{Events: []rpc.Event{event("e1", 100, "CABC")}, LatestLedger: 100}},
	}
	st := newMockStore()
	st.upsertErr = errors.New("disk full")

	ing := newTestIngester(client, st, Options{RetentionLedgers: 50})
	require.Error(t, ing.pollOnce(context.Background()))
	assert.Zero(t, st.savedState().LastLedger)
}

func TestRunStopsOnContextCancel(t *testing.T) {
	client := &mockRPC{latest: 100}
	st := newMockStore()

	ing := newTestIngester(client, st, Options{PollInterval: time.Second, RetentionLedgers: 50})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- ing.Run(ctx) }()

	cancel()

	select {
	case err := <-done:
		require.NoError(t, err, "a cancelled context is a clean shutdown, not a failure")
	case <-time.After(5 * time.Second):
		t.Fatal("ingester did not stop after its context was cancelled")
	}
}

func TestFilterBatches(t *testing.T) {
	contract := func(i int) string { return fmt.Sprintf("C%055d", i) }

	t.Run("no watched contracts matches every contract event", func(t *testing.T) {
		batches := filterBatches(nil)
		require.Len(t, batches, 1)
		require.Len(t, batches[0], 1)
		assert.Equal(t, "contract", batches[0][0].Type)
		assert.Empty(t, batches[0][0].ContractIDs)
	})

	t.Run("contracts are chunked to the per-filter cap", func(t *testing.T) {
		var contracts []string
		for i := 0; i < rpc.MaxContractIDsPerFilter+1; i++ {
			contracts = append(contracts, contract(i))
		}

		batches := filterBatches(contracts)
		require.Len(t, batches, 1)
		require.Len(t, batches[0], 2, "expected a second filter once the first is full")
		assert.Len(t, batches[0][0].ContractIDs, rpc.MaxContractIDsPerFilter)
		assert.Len(t, batches[0][1].ContractIDs, 1)
	})

	t.Run("filters are chunked into separate requests", func(t *testing.T) {
		// One more contract than a single request can carry forces a second.
		var contracts []string
		for i := 0; i < rpc.MaxWatchedContracts+1; i++ {
			contracts = append(contracts, contract(i))
		}

		batches := filterBatches(contracts)
		require.Len(t, batches, 2)
		assert.Len(t, batches[0], rpc.MaxFiltersPerRequest)
		assert.Len(t, batches[1], 1)

		// Every contract must appear exactly once across all batches.
		seen := map[string]int{}
		for _, batch := range batches {
			for _, f := range batch {
				assert.LessOrEqual(t, len(f.ContractIDs), rpc.MaxContractIDsPerFilter)
				for _, id := range f.ContractIDs {
					seen[id]++
				}
			}
		}
		assert.Len(t, seen, len(contracts))
		for id, n := range seen {
			assert.Equal(t, 1, n, "contract %s appeared %d times", id, n)
		}
	})
}

func TestNextCursorFallsBackToPagingToken(t *testing.T) {
	t.Run("top-level cursor wins", func(t *testing.T) {
		got := nextCursor(rpc.GetEventsResult{
			Cursor: "explicit",
			Events: []rpc.Event{{PagingToken: "token"}},
		})
		assert.Equal(t, "explicit", got)
	})

	t.Run("falls back to the last paging token", func(t *testing.T) {
		got := nextCursor(rpc.GetEventsResult{
			Events: []rpc.Event{{PagingToken: "first"}, {PagingToken: "last"}},
		})
		assert.Equal(t, "last", got)
	})

	t.Run("no events yields no cursor", func(t *testing.T) {
		assert.Empty(t, nextCursor(rpc.GetEventsResult{}))
	})
}

func TestPerContractCursorsMigrationFromGlobalState(t *testing.T) {
	// An existing deployment upgrading from global ingest_state (LastLedger=200)
	// with no rows in contract_ingest_state yet must seed each watched contract
	// from the global cursor and resume at ledger 201 rather than re-reading the
	// retention window.
	client := &mockRPC{latest: 250}
	st := newMockStore()
	st.state = store.IngestState{LastLedger: 200}

	ing := newTestIngester(client, st, Options{
		WatchedContracts: []string{"CAAA", "CBBB"},
		RetentionLedgers: 100,
	})
	require.NoError(t, ing.pollOnce(context.Background()))

	reqs := client.capturedRequests()
	require.Len(t, reqs, 1, "both contracts share the seeded start ledger and are read in one windowed request")
	assert.Equal(t, uint32(201), reqs[0].StartLedger)
	assert.Equal(t, uint32(251), reqs[0].EndLedger)

	for _, cid := range []string{"CAAA", "CBBB"} {
		cs, ok := st.savedContractState(cid)
		require.True(t, ok, "expected saved cursor for %s", cid)
		assert.Equal(t, int64(250), cs.LastLedger)
	}
}

func TestPerContractCursorsNewlyAddedContractBackfills(t *testing.T) {
	t.Run("default retention mode backfills newly added contract without rewinding existing ones", func(t *testing.T) {
		client := &mockRPC{latest: 500}
		st := newMockStore()
		st.contractStates["CAAA"] = store.ContractIngestState{ContractID: "CAAA", LastLedger: 480}

		ing := newTestIngester(client, st, Options{
			WatchedContracts: []string{"CAAA", "CNEW"},
			RetentionLedgers: 100,
			NewContractStart: NewContractStartRetention,
		})
		require.NoError(t, ing.pollOnce(context.Background()))

		reqs := client.capturedRequests()
		require.Len(t, reqs, 2, "existing and newly added contracts have distinct start ledgers")
		assert.Equal(t, uint32(481), reqs[0].StartLedger)
		assert.Equal(t, []string{"CAAA"}, reqs[0].Filters[0].ContractIDs)
		assert.Equal(t, uint32(400), reqs[1].StartLedger, "new contract reaches back by RetentionLedgers")
		assert.Equal(t, []string{"CNEW"}, reqs[1].Filters[0].ContractIDs)

		// On the next poll after restart, both are caught up at 500 and share one request.
		client.latest = 510
		require.NoError(t, ing.pollOnce(context.Background()))
		reqs = client.capturedRequests()
		require.Len(t, reqs, 3)
		assert.Equal(t, uint32(501), reqs[2].StartLedger)
		assert.Equal(t, []string{"CAAA", "CNEW"}, reqs[2].Filters[0].ContractIDs)
	})

	t.Run("tip mode starts newly added contract at the current tip", func(t *testing.T) {
		client := &mockRPC{latest: 500}
		st := newMockStore()
		st.contractStates["CAAA"] = store.ContractIngestState{ContractID: "CAAA", LastLedger: 500}

		ing := newTestIngester(client, st, Options{
			WatchedContracts: []string{"CAAA", "CNEW"},
			RetentionLedgers: 100,
			NewContractStart: NewContractStartTip,
		})
		require.NoError(t, ing.pollOnce(context.Background()))

		reqs := client.capturedRequests()
		require.Len(t, reqs, 1)
		assert.Equal(t, uint32(500), reqs[0].StartLedger)
		assert.Equal(t, []string{"CNEW"}, reqs[0].Filters[0].ContractIDs)
	})

	t.Run("explicit ledger mode starts newly added contract at that ledger", func(t *testing.T) {
		client := &mockRPC{latest: 500}
		st := newMockStore()
		st.contractStates["CAAA"] = store.ContractIngestState{ContractID: "CAAA", LastLedger: 500}

		ing := newTestIngester(client, st, Options{
			WatchedContracts: []string{"CAAA", "CNEW"},
			RetentionLedgers: 100,
			NewContractStart: "325",
		})
		require.NoError(t, ing.pollOnce(context.Background()))

		reqs := client.capturedRequests()
		require.Len(t, reqs, 1)
		assert.Equal(t, uint32(325), reqs[0].StartLedger)
		assert.Equal(t, []string{"CNEW"}, reqs[0].Filters[0].ContractIDs)
	})
}

type failingContractDecoder struct {
	badSymbol string
}

func (d failingContractDecoder) DecodeScVal(s string) (json.RawMessage, error) {
	if s == d.badSymbol {
		return nil, errors.New("corrupt xdr payload")
	}
	return json.Marshal(map[string]string{"symbol": s})
}

func TestPerContractFailureIsolatesHealthyContracts(t *testing.T) {
	t.Run("decode failure on one contract leaves other contract unaffected", func(t *testing.T) {
		badEvent := event("0000000150-0000000002", 150, "CBAD")
		badEvent.Value = "poison"

		client := &mockRPC{
			latest: 150,
			responses: []rpc.GetEventsResult{{
				Events: []rpc.Event{
					event("0000000150-0000000001", 150, "CGOOD"),
					badEvent,
				},
				LatestLedger: 150,
			}},
		}
		st := newMockStore()
		st.contractStates["CGOOD"] = store.ContractIngestState{ContractID: "CGOOD", LastLedger: 100}
		st.contractStates["CBAD"] = store.ContractIngestState{ContractID: "CBAD", LastLedger: 100}

		ing := New(client, st, failingContractDecoder{badSymbol: "poison"}, Options{
			WatchedContracts: []string{"CGOOD", "CBAD"},
			RetentionLedgers: 50,
		}, testLogger())

		err := ing.pollOnce(context.Background())
		require.Error(t, err, "poll should report the failing contract")
		assert.Contains(t, err.Error(), "CBAD")

		// CGOOD's event was stored and its cursor advanced to 150.
		_, ok := st.stored("0000000150-0000000001")
		assert.True(t, ok, "healthy contract event must be stored")
		goodState, _ := st.savedContractState("CGOOD")
		assert.Equal(t, int64(150), goodState.LastLedger)

		// CBAD's cursor did NOT advance past 100.
		badState, _ := st.savedContractState("CBAD")
		assert.Equal(t, int64(100), badState.LastLedger, "failing contract cursor must not advance")
	})

	t.Run("rpc error on one contract batch falls back and advances healthy contract", func(t *testing.T) {
		client := &mockRPC{
			latest: 150,
			// 1st call (combined batch) fails; 2nd call (CGOOD retry) succeeds; 3rd call (CBAD retry) fails.
			errs: []error{errors.New("batch rpc error"), nil, errors.New("cbad rpc error")},
			responses: []rpc.GetEventsResult{
				{Events: []rpc.Event{event("0000000150-0000000001", 150, "CGOOD")}, LatestLedger: 150},
			},
		}
		st := newMockStore()
		st.contractStates["CGOOD"] = store.ContractIngestState{ContractID: "CGOOD", LastLedger: 100}
		st.contractStates["CBAD"] = store.ContractIngestState{ContractID: "CBAD", LastLedger: 100}

		ing := newTestIngester(client, st, Options{
			WatchedContracts: []string{"CGOOD", "CBAD"},
			RetentionLedgers: 50,
		})

		err := ing.pollOnce(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "CBAD")

		goodState, _ := st.savedContractState("CGOOD")
		assert.Equal(t, int64(150), goodState.LastLedger, "healthy contract advances on per-contract fallback")

		badState, _ := st.savedContractState("CBAD")
		assert.Equal(t, int64(100), badState.LastLedger, "failing contract stays at its prior ledger")
	})
}
