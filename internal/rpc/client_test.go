package rpc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// capturedRequest is one decoded JSON-RPC request the fake node received.
type capturedRequest struct {
	Method string          `json:"method"`
	ID     uint64          `json:"id"`
	Params json.RawMessage `json:"params"`
}

// fakeNode is an httptest server standing in for a Stellar RPC node. handler
// returns the raw JSON body to send back for each request.
func fakeNode(t *testing.T, handler func(req capturedRequest) string) (*HTTPClient, *[]capturedRequest) {
	t.Helper()

	var captured []capturedRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var req capturedRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		captured = append(captured, req)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(handler(req)))
	}))
	t.Cleanup(srv.Close)

	return NewHTTPClient(srv.URL, srv.Client()), &captured
}

func TestGetEventsDecodesJSONFormat(t *testing.T) {
	client, captured := fakeNode(t, func(capturedRequest) string {
		return `{"jsonrpc":"2.0","id":1,"result":{
			"events":[{
				"id":"0000000123-0000000001",
				"type":"contract",
				"ledger":123,
				"ledgerClosedAt":"2024-01-01T00:00:00Z",
				"contractId":"CABC",
				"pagingToken":"0000000123-0000000001",
				"inSuccessfulContractCall":true,
				"txHash":"deadbeef",
				"txIndex":2,
				"opIndex":3,
				"topicJson":[{"symbol":"transfer"}],
				"valueJson":{"i128":"1000"}
			}],
			"latestLedger":200,
			"cursor":"0000000123-0000000001"
		}}`
	})

	res, err := client.GetEvents(context.Background(), GetEventsRequest{StartLedger: 100})
	require.NoError(t, err)

	require.Len(t, res.Events, 1)
	e := res.Events[0]
	assert.Equal(t, "0000000123-0000000001", e.ID)
	assert.Equal(t, uint32(123), e.Ledger)
	assert.Equal(t, "CABC", e.ContractID)
	assert.True(t, e.InSuccessfulContractCall)
	assert.Equal(t, int32(2), e.TxIndex)
	assert.Equal(t, int32(3), e.OpIndex)
	assert.JSONEq(t, `{"symbol":"transfer"}`, string(e.TopicJSON[0]))
	assert.JSONEq(t, `{"i128":"1000"}`, string(e.ValueJSON))
	assert.Equal(t, uint32(200), res.LatestLedger)

	// The client should have asked for the readable format up front.
	require.Len(t, *captured, 1)
	var params GetEventsRequest
	require.NoError(t, json.Unmarshal((*captured)[0].Params, &params))
	assert.Equal(t, "json", params.XDRFormat)
	assert.True(t, client.JSONXDR())
}

func TestGetEventsFallsBackToBase64(t *testing.T) {
	var fellBack atomic.Bool

	client, captured := fakeNode(t, func(req capturedRequest) string {
		var params GetEventsRequest
		require.NoError(t, json.Unmarshal(req.Params, &params))

		// Emulate a node that predates xdrFormat and rejects the field.
		if params.XDRFormat != "" {
			return `{"jsonrpc":"2.0","id":1,"error":{"code":-32602,"message":"unknown field xdrFormat"}}`
		}
		return `{"jsonrpc":"2.0","id":1,"result":{
			"events":[{"id":"1","type":"contract","ledger":5,"contractId":"CABC",
			           "topic":["AAAADw=="],"value":"AAAAAQ=="}],
			"latestLedger":9
		}}`
	})
	client.OnXDRFallback(func() { fellBack.Store(true) })

	res, err := client.GetEvents(context.Background(), GetEventsRequest{StartLedger: 1})
	require.NoError(t, err)
	require.Len(t, res.Events, 1)
	assert.Equal(t, []string{"AAAADw=="}, res.Events[0].Topic)

	// One rejected attempt plus one successful retry.
	assert.Len(t, *captured, 2)
	assert.True(t, fellBack.Load(), "expected the fallback callback to fire")
	assert.False(t, client.JSONXDR(), "expected json format to latch off")

	// Subsequent calls must not pay the retry again.
	_, err = client.GetEvents(context.Background(), GetEventsRequest{StartLedger: 2})
	require.NoError(t, err)
	assert.Len(t, *captured, 3, "expected exactly one more request, with no retry")
}

func TestGetEventsPropagatesRealErrors(t *testing.T) {
	// An error unrelated to xdrFormat must surface rather than trigger a
	// pointless fallback retry.
	client, captured := fakeNode(t, func(capturedRequest) string {
		return `{"jsonrpc":"2.0","id":1,"error":{"code":-32600,"message":"startLedger exceeds latest ledger"}}`
	})

	_, err := client.GetEvents(context.Background(), GetEventsRequest{StartLedger: 999999999})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "startLedger exceeds latest ledger")
	assert.Len(t, *captured, 1, "expected no retry for an unrelated error")
	assert.True(t, client.JSONXDR())
}

func TestGetLatestLedger(t *testing.T) {
	client, _ := fakeNode(t, func(req capturedRequest) string {
		assert.Equal(t, "getLatestLedger", req.Method)
		return `{"jsonrpc":"2.0","id":1,"result":{"id":"abc","sequence":4242,"protocolVersion":21}}`
	})

	got, err := client.GetLatestLedger(context.Background())
	require.NoError(t, err)
	assert.Equal(t, uint32(4242), got.Sequence)
	assert.Equal(t, 21, got.ProtocolVersion)
}

func TestGetHealth(t *testing.T) {
	client, _ := fakeNode(t, func(req capturedRequest) string {
		assert.Equal(t, "getHealth", req.Method)
		return `{"jsonrpc":"2.0","id":1,"result":{
			"status":"healthy","latestLedger":100,"oldestLedger":10,"ledgerRetentionWindow":90}}`
	})

	got, err := client.GetHealth(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "healthy", got.Status)
	assert.Equal(t, uint32(90), got.LedgerRetentionWindow)
}

func TestCallRejectsNonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "upstream is down", http.StatusBadGateway)
	}))
	t.Cleanup(srv.Close)

	_, err := NewHTTPClient(srv.URL, srv.Client()).GetHealth(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected status")
}

func TestCallRejectsMalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("this is not json"))
	}))
	t.Cleanup(srv.Close)

	_, err := NewHTTPClient(srv.URL, srv.Client()).GetHealth(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decoding getHealth response")
}

func TestRequestIDsIncrease(t *testing.T) {
	client, captured := fakeNode(t, func(capturedRequest) string {
		return `{"jsonrpc":"2.0","id":1,"result":{"status":"healthy"}}`
	})

	for i := 0; i < 3; i++ {
		_, err := client.GetHealth(context.Background())
		require.NoError(t, err)
	}

	require.Len(t, *captured, 3)
	assert.Equal(t, uint64(1), (*captured)[0].ID)
	assert.Equal(t, uint64(2), (*captured)[1].ID)
	assert.Equal(t, uint64(3), (*captured)[2].ID)
}

func TestContextCancellationIsRespected(t *testing.T) {
	client, _ := fakeNode(t, func(capturedRequest) string {
		return `{"jsonrpc":"2.0","id":1,"result":{"status":"healthy"}}`
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.GetHealth(ctx)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}
