package web

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sorotrail/sorolens/internal/source"
)

const testContract = "CDLZFC3SYJYDZT7K67VZ75HPJVIEUVNIXF47ZG2FB2RMQQVU2HHGCYSC"

// fakeSource is a canned source.EventSource for rendering tests.
type fakeSource struct {
	contracts     source.ContractPage
	events        source.EventPage
	event         source.Event
	stats         source.Stats
	contractStats source.ContractStats
	status        source.Status
	err           error

	lastEventQuery    source.EventQuery
	lastContractQuery source.ContractQuery
	listContractsFn   func(context.Context, source.ContractQuery) (source.ContractPage, error)
}

func (f *fakeSource) ListContracts(ctx context.Context, q source.ContractQuery) (source.ContractPage, error) {
	f.lastContractQuery = q
	if f.listContractsFn != nil {
		return f.listContractsFn(ctx, q)
	}
	return f.contracts, f.err
}

func (f *fakeSource) ListEvents(_ context.Context, q source.EventQuery) (source.EventPage, error) {
	f.lastEventQuery = q
	return f.events, f.err
}

func (f *fakeSource) GetEvent(context.Context, string) (source.Event, error) {
	return f.event, f.err
}

func (f *fakeSource) ContractStats(context.Context, string) (source.ContractStats, error) {
	return f.contractStats, f.err
}

func (f *fakeSource) Stats(context.Context) (source.Stats, error) { return f.stats, f.err }
func (f *fakeSource) Status(context.Context) source.Status        { return f.status }

// sampleEvent is a realistic decoded event used across the rendering tests.
func sampleEvent() source.Event {
	return source.Event{
		ID:               "0000000100-0000000001",
		ContractID:       testContract,
		Ledger:           100,
		Type:             "contract",
		TxHash:           "b1946ac92492d2347c6235b4d2611184",
		TxIndex:          1,
		OpIndex:          2,
		InSuccessfulCall: true,
		Topics:           json.RawMessage(`[{"symbol":"transfer"},{"address":"GABC"}]`),
		Value:            json.RawMessage(`{"i128":"1000"}`),
		LedgerClosedAt:   time.Unix(1700000000, 0).UTC(),
		CreatedAt:        time.Unix(1700000100, 0).UTC(),
	}
}

func newTestServer(t *testing.T, src source.EventSource) http.Handler {
	t.Helper()
	s, err := New(src, slog.New(slog.NewTextHandler(io.Discard, nil)))
	require.NoError(t, err)
	return s.Routes()
}

func get(t *testing.T, h http.Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

// TestTemplatesParse is the guard against a template typo shipping: templates
// are embedded and only parsed at startup, so a broken one would otherwise
// surface as a runtime failure in production rather than a failing build.
func TestTemplatesParse(t *testing.T) {
	_, err := New(&fakeSource{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	require.NoError(t, err)
}

func TestIndexRenders(t *testing.T) {
	src := &fakeSource{
		stats: source.Stats{
			TotalEvents:   1234,
			ContractCount: 7,
			FirstLedger:   100,
			LastLedger:    900,
			TypeBreakdown: []source.TypeCount{{Type: "contract", Count: 1200}},
		},
		events: source.EventPage{Events: []source.Event{sampleEvent()}},
		status: source.Status{Mode: "rpc", Healthy: true, RetentionNote: "only while running"},
	}

	rec := get(t, newTestServer(t, src), "/")
	require.Equal(t, http.StatusOK, rec.Code)

	body := rec.Body.String()
	assert.Contains(t, body, "1234", "total event count should be shown")
	assert.Contains(t, body, "SoroLens")
	// The decoded event name, not the raw JSON wrapper, is what users need.
	assert.Contains(t, body, "transfer")
	// The retention caveat must be visible so an empty explorer is explicable.
	assert.Contains(t, body, "only while running")
}

func TestIndexShowsUnhealthyBanner(t *testing.T) {
	src := &fakeSource{
		status: source.Status{Mode: "rpc", Healthy: false, Detail: "database unreachable"},
		events: source.EventPage{},
	}

	rec := get(t, newTestServer(t, src), "/")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "database unreachable")
}

func TestContractsPageRenders(t *testing.T) {
	src := &fakeSource{
		contracts: source.ContractPage{
			Contracts: []source.Contract{{
				ID:           testContract,
				EventCount:   42,
				FirstLedger:  100,
				LastLedger:   200,
				LastActivity: time.Unix(1700000000, 0).UTC(),
			}},
			NextCursor: "abc",
		},
		status: source.Status{Healthy: true},
	}

	rec := get(t, newTestServer(t, src), "/contracts")
	require.Equal(t, http.StatusOK, rec.Code)

	body := rec.Body.String()
	assert.Contains(t, body, "42")
	assert.Contains(t, body, "/contracts/"+testContract, "each row should link to the contract")
	assert.Contains(t, body, "Next page", "a next cursor should produce a paging link")
	assert.Contains(t, body, "more contracts are available", "the page should state when the list is incomplete")
	assert.Contains(t, body, `aria-label="Next page of contracts"`)
	assert.Equal(t, source.DefaultLimit, src.lastContractQuery.Limit, "page size should come from source.NormalizeLimit")
}

func TestContractsPaginationPreservesFilterAndHandlesInvalidCursor(t *testing.T) {
	contractA := "CDLZFC3SYJYDZT7K67VZ75HPJVIEUVNIXF47ZG2FB2RMQQVU2HHGCYSC"
	contractB := "CDZZXVGUCLBUTE3WDD6TF2NXPTBPOYTVPPBBIZMSPVIQLUG226XJ4PAN"

	src := &fakeSource{
		status: source.Status{Healthy: true},
		listContractsFn: func(_ context.Context, q source.ContractQuery) (source.ContractPage, error) {
			switch q.Cursor {
			case "":
				return source.ContractPage{
					Contracts:  []source.Contract{{ID: contractA, EventCount: 10, FirstLedger: 1, LastLedger: 20}},
					NextCursor: "opaque-cursor-page-2",
				}, nil
			case "opaque-cursor-page-2":
				return source.ContractPage{
					Contracts: []source.Contract{{ID: contractB, EventCount: 5, FirstLedger: 1, LastLedger: 10}},
				}, nil
			default:
				return source.ContractPage{}, source.ErrInvalidCursor
			}
		},
	}
	h := newTestServer(t, src)

	// Page 1 with a search filter.
	rec1 := get(t, h, "/contracts?search=CD")
	require.Equal(t, http.StatusOK, rec1.Code)
	body1 := rec1.Body.String()
	assert.Contains(t, body1, "/contracts/"+contractA)
	assert.NotContains(t, body1, "/contracts/"+contractB)
	assert.Contains(t, body1, "/contracts?cursor=opaque-cursor-page-2&amp;search=CD",
		"the search filter must survive in the next-page link")

	// Page 2 via the opaque cursor.
	rec2 := get(t, h, "/contracts?cursor=opaque-cursor-page-2&search=CD")
	require.Equal(t, http.StatusOK, rec2.Code)
	assert.Equal(t, "CD", src.lastContractQuery.Search)
	assert.Equal(t, "opaque-cursor-page-2", src.lastContractQuery.Cursor)
	body2 := rec2.Body.String()
	assert.Contains(t, body2, "/contracts/"+contractB)
	assert.NotContains(t, body2, "/contracts/"+contractA)
	assert.Contains(t, body2, `href="/contracts?search=CD"`, "continuation pages link back to the first page with the filter intact")

	// Invalid or stale cursor renders a 400 page rather than a 500.
	recBad := get(t, h, "/contracts?cursor=stale-cursor")
	require.Equal(t, http.StatusBadRequest, recBad.Code)
	assert.Contains(t, recBad.Body.String(), "Bad request")

	// Partial endpoint also renders just the table fragment.
	recPartial := get(t, h, "/partials/contracts?search=CD")
	require.Equal(t, http.StatusOK, recPartial.Code)
	assert.NotContains(t, recPartial.Body.String(), "<html")
	assert.Contains(t, recPartial.Body.String(), "/contracts/"+contractA)
}

func TestContractsPageEmptyState(t *testing.T) {
	src := &fakeSource{contracts: source.ContractPage{}, status: source.Status{Healthy: true}}

	rec := get(t, newTestServer(t, src), "/contracts")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "No contracts to show")
}

func TestContractDetailRenders(t *testing.T) {
	src := &fakeSource{
		contractStats: source.ContractStats{
			ContractID:    testContract,
			TotalEvents:   42,
			FirstLedger:   100,
			LastLedger:    200,
			TypeBreakdown: []source.TypeCount{{Type: "contract", Count: 42}},
		},
		events: source.EventPage{Events: []source.Event{sampleEvent()}},
		status: source.Status{Healthy: true},
	}

	rec := get(t, newTestServer(t, src), "/contracts/"+testContract)
	require.Equal(t, http.StatusOK, rec.Code)

	body := rec.Body.String()
	assert.Contains(t, body, testContract)
	assert.Contains(t, body, "transfer")
	assert.Contains(t, body, "Filters", "the filter form should be present")
	assert.Contains(t, body, `id="events"`, "the htmx swap target should exist")
}

func TestContractDetailRejectsBadID(t *testing.T) {
	src := &fakeSource{status: source.Status{Healthy: true}}

	rec := get(t, newTestServer(t, src), "/contracts/not-a-contract")
	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "Invalid contract ID")
}

func TestContractDetailPassesFiltersThrough(t *testing.T) {
	src := &fakeSource{
		events: source.EventPage{},
		status: source.Status{Healthy: true},
	}

	rec := get(t, newTestServer(t, src), "/contracts/"+testContract+"?type=contract&from_ledger=50&to_ledger=150")
	require.Equal(t, http.StatusOK, rec.Code)

	q := src.lastEventQuery
	assert.Equal(t, testContract, q.ContractID)
	assert.Equal(t, "contract", q.Type)
	assert.Equal(t, int64(50), q.FromLedger)
	assert.Equal(t, int64(150), q.ToLedger)
}

func TestEventDetailRenders(t *testing.T) {
	src := &fakeSource{event: sampleEvent(), status: source.Status{Healthy: true}}

	rec := get(t, newTestServer(t, src), "/events/0000000100-0000000001")
	require.Equal(t, http.StatusOK, rec.Code)

	body := rec.Body.String()
	// Everything the event detail page promises to show.
	assert.Contains(t, body, "transfer", "decoded topic")
	assert.Contains(t, body, "GABC", "decoded address topic")
	assert.Contains(t, body, "1000", "decoded value")
	assert.Contains(t, body, "b1946ac92492d2347c6235b4d2611184", "transaction hash")
	assert.Contains(t, body, testContract, "contract link")
	assert.Contains(t, body, "Raw decoded values")
}

func TestEventDetailShowsFailedCall(t *testing.T) {
	e := sampleEvent()
	e.InSuccessfulCall = false
	src := &fakeSource{event: e, status: source.Status{Healthy: true}}

	rec := get(t, newTestServer(t, src), "/events/"+e.ID)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "failed call")
}

func TestEventNotFound(t *testing.T) {
	src := &fakeSource{err: source.ErrNotFound, status: source.Status{Healthy: true}}

	rec := get(t, newTestServer(t, src), "/events/missing")
	require.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), "Event not found")
}

func TestSearchRouting(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		location string
	}{
		{"contract ID goes to the contract page", testContract, "/contracts/" + testContract},
		{"lowercase contract ID is normalized", "cdlzfc3syjydzt7k67vz75hpjvieuvnixf47zg2fb2rmqqvu2hhgcysc", "/contracts/" + testContract},
		{"event TOID goes to the event page", "0000000100-0000000001", "/events/0000000100-0000000001"},
		{"plain digits are treated as an event ID", "12345", "/events/12345"},
		{"a partial ID falls back to a contract search", "CDLZ", "/contracts?search=CDLZ"},
		{"empty query lists contracts", "", "/contracts"},
	}

	h := newTestServer(t, &fakeSource{status: source.Status{Healthy: true}})
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := get(t, h, "/search?q="+tt.query)
			require.Equal(t, http.StatusSeeOther, rec.Code)
			assert.Equal(t, tt.location, rec.Header().Get("Location"))
		})
	}
}

func TestPartialEventsReturnsFragmentOnly(t *testing.T) {
	// htmx swaps this in, so it must be a bare fragment: no layout, no <html>.
	src := &fakeSource{
		events: source.EventPage{Events: []source.Event{sampleEvent()}, NextCursor: "next"},
		status: source.Status{Healthy: true},
	}

	rec := get(t, newTestServer(t, src), "/partials/events?contract_id="+testContract)
	require.Equal(t, http.StatusOK, rec.Code)

	body := rec.Body.String()
	assert.NotContains(t, body, "<html", "a partial must not include the layout")
	assert.NotContains(t, body, "<header", "a partial must not include the navigation")
	assert.Contains(t, body, "transfer")
	assert.Contains(t, body, "Load more", "a next cursor should render the load-more control")
	assert.Contains(t, body, "hx-get", "the load-more control drives htmx")
}

func TestPartialEventsRejectsBadContract(t *testing.T) {
	src := &fakeSource{status: source.Status{Healthy: true}}

	rec := get(t, newTestServer(t, src), "/partials/events?contract_id=bogus")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestPartialEventsEmptyState(t *testing.T) {
	src := &fakeSource{events: source.EventPage{}, status: source.Status{Healthy: true}}

	rec := get(t, newTestServer(t, src), "/partials/events")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "No events match these filters")
}

func TestUnknownPathRenders404(t *testing.T) {
	src := &fakeSource{status: source.Status{Healthy: true}}

	rec := get(t, newTestServer(t, src), "/no/such/page")
	require.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), "Not found")
}

func TestHTMLIsEscaped(t *testing.T) {
	// Event data is attacker-controlled: a contract can emit any string it
	// likes, so it must never be rendered as markup.
	e := sampleEvent()
	e.Topics = json.RawMessage(`[{"symbol":"<script>alert(1)</script>"}]`)
	src := &fakeSource{event: e, status: source.Status{Healthy: true}}

	rec := get(t, newTestServer(t, src), "/events/"+e.ID)
	require.Equal(t, http.StatusOK, rec.Code)

	body := rec.Body.String()
	assert.NotContains(t, body, "<script>alert(1)</script>", "event data must be escaped")
	assert.Contains(t, body, "&lt;script&gt;")
}

func TestClassifySearch(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  searchKind
	}{
		{"contract ID", testContract, searchContract},
		{"event TOID", "0000000100-0000000001", searchEvent},
		{"plain digits", "12345", searchEvent},
		{"trailing dash is not an ID", "12345-", searchUnknown},
		{"double dash is not an ID", "1-2-3", searchUnknown},
		{"empty", "", searchUnknown},
		{"partial contract", "CDLZ", searchUnknown},
		{"words", "hello world", searchUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, classifySearch(tt.query))
		})
	}
}

func TestFavicon(t *testing.T) {
	rec := get(t, newTestServer(t, &fakeSource{}), "/favicon.ico")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "image/svg+xml", rec.Header().Get("Content-Type"))
	assert.NotEmpty(t, rec.Body.String())
}

func TestNavHighlightsActivePage(t *testing.T) {
	src := &fakeSource{
		contracts:     source.ContractPage{Contracts: []source.Contract{{ID: testContract}}},
		contractStats: source.ContractStats{ContractID: testContract},
		status:        source.Status{Healthy: true},
	}

	tests := []struct {
		path   string
		active string // link text expected to carry class="active"
	}{
		{"/", "Overview"},
		{"/contracts", "Contracts"},
		{"/contracts/" + testContract, "Contracts"},
	}
	linkNames := []string{"Overview", "Contracts"}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			rec := get(t, newTestServer(t, src), tt.path)
			require.Equal(t, http.StatusOK, rec.Code)
			html := rec.Body.String()

			for _, name := range linkNames {
				idx := strings.Index(html, ">"+name+"</a>")
				require.GreaterOrEqual(t, idx, 0, "nav link %q not found", name)
				tagStart := strings.LastIndex(html[:idx], "<a ")
				tag := html[tagStart:idx]
				hasActive := strings.Contains(tag, `class="active"`)
				wantActive := name == tt.active
				assert.Equal(t, wantActive, hasActive, "%s: nav link %q active=%v, want %v (tag: %s)", tt.path, name, hasActive, wantActive, tag)
			}
		})
	}
}

func TestNavNotHighlightedOnEventOrErrorPages(t *testing.T) {
	src := &fakeSource{
		event:  sampleEvent(),
		status: source.Status{Healthy: true},
	}
	linkNames := []string{"Overview", "Contracts"}

	for _, path := range []string{"/events/" + sampleEvent().ID, "/no-such-page"} {
		t.Run(path, func(t *testing.T) {
			rec := get(t, newTestServer(t, src), path)
			html := rec.Body.String()
			for _, name := range linkNames {
				idx := strings.Index(html, ">"+name+"</a>")
				require.GreaterOrEqual(t, idx, 0, "nav link %q not found", name)
				tagStart := strings.LastIndex(html[:idx], "<a ")
				tag := html[tagStart:idx]
				assert.NotContains(t, tag, `class="active"`, "%s: nav link %q must not be highlighted", path, name)
			}
		})
	}
}
