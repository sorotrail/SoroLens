// Package decode turns Soroban ScVal payloads into queryable JSON and into
// short human-readable strings for display.
//
// Contract events arrive from the RPC in one of two shapes. When the node
// supports xdrFormat "json", topics and values are already JSON and pass
// through untouched; otherwise they are base64-encoded XDR and are decoded
// locally through the Decoder interface.
//
// contributors: this package is a deliberately thin, replaceable seam. Richer
// decoding — recognizing SEP-41 token transfer events and emitting normalized
// shapes, or decoding against a contract's spec into named fields — belongs in
// a new Decoder implementation or a layer above, not in a wider interface.
package decode

import (
	"encoding/json"
	"fmt"
)

// Decoder converts a single base64-encoded XDR ScVal into JSON.
type Decoder interface {
	DecodeScVal(base64XDR string) (json.RawMessage, error)
}

// RawEvent is the subset of an RPC event this package needs. Keeping it local
// avoids a dependency on the rpc package, so the decoder stays usable against
// any source of ScVals.
type RawEvent struct {
	// Topic and Value hold base64 XDR, used when the node does not support
	// xdrFormat "json".
	Topic []string
	Value string
	// TopicJSON and ValueJSON hold node-decoded JSON and take precedence.
	TopicJSON []json.RawMessage
	ValueJSON json.RawMessage
}

// TopicsValue extracts an event's topics as a JSON array and its value as a
// JSON value, preferring the node-decoded JSON and falling back to local XDR
// decoding through d.
func TopicsValue(d Decoder, e RawEvent) (topics, value json.RawMessage, err error) {
	switch {
	case e.TopicJSON != nil:
		topics, err = json.Marshal(e.TopicJSON)
		if err != nil {
			return nil, nil, fmt.Errorf("re-marshaling topics: %w", err)
		}
	default:
		decoded := make([]json.RawMessage, 0, len(e.Topic))
		for i, t := range e.Topic {
			v, err := d.DecodeScVal(t)
			if err != nil {
				return nil, nil, fmt.Errorf("decoding topic %d: %w", i, err)
			}
			decoded = append(decoded, v)
		}
		topics, err = json.Marshal(decoded)
		if err != nil {
			return nil, nil, fmt.Errorf("marshaling topics: %w", err)
		}
	}

	switch {
	case e.ValueJSON != nil:
		value = e.ValueJSON
	case e.Value != "":
		value, err = d.DecodeScVal(e.Value)
		if err != nil {
			return nil, nil, fmt.Errorf("decoding value: %w", err)
		}
	default:
		value = json.RawMessage("null")
	}
	return topics, value, nil
}
