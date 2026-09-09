package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Client is the Stellar RPC surface SoroLens uses. The ingester depends on
// this interface rather than on HTTPClient, so tests can supply a fake node.
//
// contributors: adding a method here means adding it to every fake in the
// tests too. Prefer a separate narrow interface when only one caller needs it.
type Client interface {
	GetEvents(ctx context.Context, req GetEventsRequest) (GetEventsResult, error)
	GetLatestLedger(ctx context.Context) (LatestLedger, error)
	GetHealth(ctx context.Context) (Health, error)
}

// DefaultTimeout bounds a single JSON-RPC round trip.
const DefaultTimeout = 30 * time.Second

// HTTPClient is the JSON-RPC 2.0 implementation of Client.
type HTTPClient struct {
	url  string
	http *http.Client

	// jsonXDR tracks whether the node accepts xdrFormat:"json". It starts
	// optimistic and latches off permanently the first time a request is
	// rejected for it, so at most one request per process pays the retry.
	jsonXDR atomic.Bool
	// reqID supplies monotonically increasing JSON-RPC request IDs.
	reqID atomic.Uint64
	// fallbackOnce guards the log-worthy transition to base64.
	fallbackOnce sync.Once
	// onFallback, when set, is called the first time the client gives up on
	// xdrFormat:"json". main wires this to a log line.
	onFallback func()
}

// NewHTTPClient returns a Client talking to the node at url.
func NewHTTPClient(url string, httpClient *http.Client) *HTTPClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: DefaultTimeout}
	}
	c := &HTTPClient{url: url, http: httpClient}
	c.jsonXDR.Store(true)
	return c
}

// OnXDRFallback registers a callback invoked once, if and when the client
// stops asking for xdrFormat:"json" because the node rejected it.
func (c *HTTPClient) OnXDRFallback(fn func()) { c.onFallback = fn }

// JSONXDR reports whether the client is still requesting xdrFormat:"json".
func (c *HTTPClient) JSONXDR() bool { return c.jsonXDR.Load() }

type jsonRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      uint64 `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (e *jsonRPCError) Error() string {
	if e.Data != nil {
		return fmt.Sprintf("rpc error %d: %s (%v)", e.Code, e.Message, e.Data)
	}
	return fmt.Sprintf("rpc error %d: %s", e.Code, e.Message)
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      uint64          `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *jsonRPCError   `json:"error"`
}

// GetEvents fetches one page of contract events. The caller drives pagination
// by following the returned cursor.
func (c *HTTPClient) GetEvents(ctx context.Context, req GetEventsRequest) (GetEventsResult, error) {
	if c.jsonXDR.Load() {
		req.XDRFormat = "json"
	} else {
		req.XDRFormat = ""
	}

	var out GetEventsResult
	err := c.call(ctx, "getEvents", req, &out)
	if err == nil {
		return out, nil
	}

	// Nodes that predate xdrFormat reject the field outright. Latch it off and
	// retry once in base64; the decoder handles either shape.
	if c.jsonXDR.Load() && isXDRFormatRejection(err) {
		c.jsonXDR.Store(false)
		if c.onFallback != nil {
			c.fallbackOnce.Do(c.onFallback)
		}
		req.XDRFormat = ""
		out = GetEventsResult{}
		if err := c.call(ctx, "getEvents", req, &out); err != nil {
			return GetEventsResult{}, err
		}
		return out, nil
	}
	return GetEventsResult{}, err
}

// GetLatestLedger returns the node's current ledger.
func (c *HTTPClient) GetLatestLedger(ctx context.Context) (LatestLedger, error) {
	var out LatestLedger
	if err := c.call(ctx, "getLatestLedger", nil, &out); err != nil {
		return LatestLedger{}, err
	}
	return out, nil
}

// GetHealth returns the node's health and its event retention window.
func (c *HTTPClient) GetHealth(ctx context.Context) (Health, error) {
	var out Health
	if err := c.call(ctx, "getHealth", nil, &out); err != nil {
		return Health{}, err
	}
	return out, nil
}

func (c *HTTPClient) call(ctx context.Context, method string, params, out any) error {
	body, err := json.Marshal(jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      c.reqID.Add(1),
		Method:  method,
		Params:  params,
	})
	if err != nil {
		return fmt.Errorf("encoding %s request: %w", method, err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("building %s request: %w", method, err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return fmt.Errorf("calling %s: %w", method, err)
	}
	defer resp.Body.Close()

	// Cap the response so a misbehaving or misaddressed endpoint cannot make
	// the ingester read without bound.
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return fmt.Errorf("reading %s response: %w", method, err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: unexpected status %s: %s", method, resp.Status, truncate(string(raw), 256))
	}

	var rpcResp jsonRPCResponse
	if err := json.Unmarshal(raw, &rpcResp); err != nil {
		return fmt.Errorf("decoding %s response: %w (body: %s)", method, err, truncate(string(raw), 256))
	}
	if rpcResp.Error != nil {
		return fmt.Errorf("%s: %w", method, rpcResp.Error)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(rpcResp.Result, out); err != nil {
		return fmt.Errorf("decoding %s result: %w", method, err)
	}
	return nil
}

// isXDRFormatRejection reports whether err looks like the node objecting to
// the xdrFormat parameter rather than to the query itself.
func isXDRFormatRejection(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "xdrformat") ||
		(strings.Contains(msg, "invalid") && strings.Contains(msg, "param")) ||
		strings.Contains(msg, "unknown field")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
