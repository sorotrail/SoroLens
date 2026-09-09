// Package rpc talks to a Stellar RPC node over JSON-RPC 2.0. It is used only
// in standalone mode, where SoroLens ingests events itself.
package rpc

import (
	"encoding/json"
	"time"
)

// Limits imposed by the getEvents method. The ingester batches watched
// contracts across filters and requests so it never exceeds them.
const (
	// MaxFiltersPerRequest is the hard cap on filters in one getEvents call.
	MaxFiltersPerRequest = 5
	// MaxContractIDsPerFilter is the cap on contractIds within one filter.
	MaxContractIDsPerFilter = 5
	// DefaultEventsLimit is the page size requested from getEvents.
	DefaultEventsLimit = 100
	// MaxWatchedContracts is how many contracts one poll can cover, given the
	// filter and contract-ID caps above.
	MaxWatchedContracts = MaxFiltersPerRequest * MaxContractIDsPerFilter
)

// EventFilter narrows getEvents results. Within a filter contractIds are
// OR-ed; a filter carrying both contractIds and topics matches their AND.
type EventFilter struct {
	Type        string     `json:"type,omitempty"` // contract, system or diagnostic
	ContractIDs []string   `json:"contractIds,omitempty"`
	Topics      [][]string `json:"topics,omitempty"`
}

// Pagination controls getEvents paging. When Cursor is set, StartLedger must
// be omitted from the same request — the node rejects both together.
type Pagination struct {
	Cursor string `json:"cursor,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

// GetEventsRequest are the params for the getEvents method.
type GetEventsRequest struct {
	StartLedger uint32        `json:"startLedger,omitempty"` // inclusive
	EndLedger   uint32        `json:"endLedger,omitempty"`   // exclusive
	Filters     []EventFilter `json:"filters,omitempty"`
	Pagination  *Pagination   `json:"pagination,omitempty"`
	// XDRFormat is "json" or "base64". The client sets this itself and falls
	// back to base64 against node versions that reject "json".
	XDRFormat string `json:"xdrFormat,omitempty"`
}

// Event is one contract event from getEvents. Depending on the xdrFormat the
// node supports, topics and values arrive either as base64 XDR (Topic, Value)
// or as ready-to-read JSON (TopicJSON, ValueJSON).
type Event struct {
	ID                       string    `json:"id"` // TOID-based, unique per event
	Type                     string    `json:"type"`
	Ledger                   uint32    `json:"ledger"`
	LedgerClosedAt           time.Time `json:"ledgerClosedAt"`
	ContractID               string    `json:"contractId"`
	PagingToken              string    `json:"pagingToken"`
	InSuccessfulContractCall bool      `json:"inSuccessfulContractCall"`
	TxHash                   string    `json:"txHash"`
	// TxIndex and OpIndex locate the event within its ledger. Older nodes omit
	// them, in which case they stay zero.
	TxIndex int32 `json:"txIndex"`
	OpIndex int32 `json:"opIndex"`

	Topic []string `json:"topic,omitempty"` // base64 XDR ScVals
	Value string   `json:"value,omitempty"` // base64 XDR ScVal

	TopicJSON []json.RawMessage `json:"topicJson,omitempty"`
	ValueJSON json.RawMessage   `json:"valueJson,omitempty"`
}

// GetEventsResult is the getEvents response. Newer nodes return a top-level
// cursor for the next page; when it is absent the last event's PagingToken
// serves the same purpose.
type GetEventsResult struct {
	Events       []Event `json:"events"`
	LatestLedger uint32  `json:"latestLedger"`
	OldestLedger uint32  `json:"oldestLedger,omitempty"`
	Cursor       string  `json:"cursor,omitempty"`
}

// LatestLedger is the getLatestLedger response.
type LatestLedger struct {
	ID              string `json:"id"`
	Sequence        uint32 `json:"sequence"`
	ProtocolVersion int    `json:"protocolVersion"`
}

// Health is the getHealth response. LedgerRetentionWindow is how many ledgers
// of events the node still holds — the reason standalone mode can only capture
// history while it is running.
type Health struct {
	Status                string `json:"status"`
	LatestLedger          uint32 `json:"latestLedger"`
	OldestLedger          uint32 `json:"oldestLedger"`
	LedgerRetentionWindow uint32 `json:"ledgerRetentionWindow"`
}
