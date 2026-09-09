// Package sorotrailsource implements source.EventSource for upstream mode,
// reading from a SoroTrail indexer's HTTP API instead of polling Stellar RPC.
//
// SoroTrail stores events durably, so upstream mode can show history the RPC
// has already dropped. Two differences between SoroTrail's API and what
// SoroLens's UI needs shape this package, and both are worked around here
// rather than hidden:
//
//  1. SoroTrail paginates ascending by event ID only. SoroLens shows newest
//     first, so scan.go walks ledger windows backwards and reverses locally.
//  2. SoroTrail has no contracts endpoint. The contracts list is therefore
//     derived from a bounded scan of recent events and reported as
//     approximate, so nobody reads a partial view as a complete one.
//
// contributors: both would collapse into simple pass-through calls if SoroTrail
// gained a descending order parameter and a GET /contracts endpoint. Those are
// tracked as upstream issues and are a good first contribution.
package sorotrailsource

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/sorotrail/sorolens/internal/source"
)

// DefaultTimeout bounds a single call to the SoroTrail API.
const DefaultTimeout = 30 * time.Second

// maxUpstreamLimit is SoroTrail's own per-page cap. Requesting more is
// rejected, so the scanner never asks for more than this.
const maxUpstreamLimit = 200

// apiClient is the thin HTTP layer over SoroTrail's endpoints. It is an
// interface so the scanner and the EventSource can be tested against a stub
// without standing up an HTTP server.
type apiClient interface {
	// Events fetches one ascending page. cursor continues a previous page.
	Events(ctx context.Context, q upstreamQuery) (upstreamPage, error)
	// Event fetches a single event by ID.
	Event(ctx context.Context, id string) (source.Event, error)
	// Stats fetches SoroTrail's own summary.
	Stats(ctx context.Context) (upstreamStats, error)
	// Health fetches SoroTrail's health.
	Health(ctx context.Context) (upstreamHealth, error)
}

// upstreamQuery mirrors the query parameters SoroTrail's /events accepts.
type upstreamQuery struct {
	ContractID string
	Type       string
	Topic      json.RawMessage
	FromLedger int64
	ToLedger   int64
	Cursor     string
	Limit      int
}

// upstreamPage is SoroTrail's /events response: {"events": [...], "cursor": "..."}.
type upstreamPage struct {
	Events []source.Event `json:"events"`
	Cursor string         `json:"cursor"`
}

// upstreamStats is SoroTrail's /stats response.
type upstreamStats struct {
	TotalEvents        int64 `json:"total_events"`
	LastIngestedLedger int64 `json:"last_ingested_ledger"`
	ContractCount      int64 `json:"contract_count"`
	WatchedContracts   int64 `json:"watched_contracts"`
}

// upstreamHealth is SoroTrail's /health response.
type upstreamHealth struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks"`
}

// httpAPI is the real apiClient, talking to a SoroTrail instance.
type httpAPI struct {
	baseURL string
	http    *http.Client
}

func newHTTPAPI(baseURL string, client *http.Client) *httpAPI {
	if client == nil {
		client = &http.Client{Timeout: DefaultTimeout}
	}
	return &httpAPI{baseURL: baseURL, http: client}
}

func (a *httpAPI) Events(ctx context.Context, q upstreamQuery) (upstreamPage, error) {
	params := url.Values{}
	if q.ContractID != "" {
		params.Set("contract_id", q.ContractID)
	}
	if q.Type != "" {
		params.Set("type", q.Type)
	}
	if len(q.Topic) > 0 {
		params.Set("topic", string(q.Topic))
	}
	if q.FromLedger > 0 {
		params.Set("from_ledger", strconv.FormatInt(q.FromLedger, 10))
	}
	if q.ToLedger > 0 {
		params.Set("to_ledger", strconv.FormatInt(q.ToLedger, 10))
	}
	if q.Cursor != "" {
		params.Set("cursor", q.Cursor)
	}
	if q.Limit > 0 {
		params.Set("limit", strconv.Itoa(min(q.Limit, maxUpstreamLimit)))
	}

	var page upstreamPage
	if err := a.get(ctx, "/events", params, &page); err != nil {
		return upstreamPage{}, err
	}
	return page, nil
}

func (a *httpAPI) Event(ctx context.Context, id string) (source.Event, error) {
	var e source.Event
	if err := a.get(ctx, "/events/"+url.PathEscape(id), nil, &e); err != nil {
		return source.Event{}, err
	}
	return e, nil
}

func (a *httpAPI) Stats(ctx context.Context) (upstreamStats, error) {
	var s upstreamStats
	if err := a.get(ctx, "/stats", nil, &s); err != nil {
		return upstreamStats{}, err
	}
	return s, nil
}

func (a *httpAPI) Health(ctx context.Context) (upstreamHealth, error) {
	var h upstreamHealth
	// SoroTrail answers /health with 503 when degraded, and the body still
	// carries the detail we want to show, so that status is not an error here.
	err := a.get(ctx, "/health", nil, &h)
	if err != nil && h.Status == "" {
		return upstreamHealth{}, err
	}
	return h, nil
}

func (a *httpAPI) get(ctx context.Context, path string, params url.Values, out any) error {
	endpoint := a.baseURL + path
	if len(params) > 0 {
		endpoint += "?" + params.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("building request for %s: %w", path, err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := a.http.Do(req)
	if err != nil {
		return fmt.Errorf("calling sorotrail %s: %w", path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return fmt.Errorf("reading sorotrail %s response: %w", path, err)
	}

	if resp.StatusCode == http.StatusNotFound {
		return source.ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		// Decode anyway: callers such as Health want the body's detail even
		// when the status is not 200.
		if out != nil {
			_ = json.Unmarshal(body, out)
		}
		return fmt.Errorf("sorotrail %s: unexpected status %s", path, resp.Status)
	}

	if out == nil {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decoding sorotrail %s response: %w", path, err)
	}
	return nil
}
