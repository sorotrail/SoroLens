package api

import (
	"encoding/json"

	"github.com/sorotrail/sorolens/internal/source"
)

// SEP-41 is Stellar's token contract interface. A token implementing it
// emits these events, with topics at fixed positions:
//
//	transfer  {sym:"transfer", addr:from, addr:to}        value: i128 amount
//	mint      {sym:"mint",     addr:admin, addr:to}        value: i128 amount
//	burn      {sym:"burn",     addr:admin, addr:from}      value: i128 amount
//	clawback  {sym:"clawback", addr:admin, addr:from}      value: i128 amount
//	set_admin {sym:"set_admin", addr:old, addr:new}        value: void
//
// TokenInfo is the API's annotation of such an event: the event name and
// its semantic slots, so a client doesn't need to know the layout to say
// "X sent Y to Z". It appears as a "token" field on event responses when
// the event matches the layout.
type TokenInfo struct {
	Event string `json:"event"`
	// From is the outgoing/admin side, To the incoming side, per event:
	// transfer from=sender; mint from=admin, to=recipient; burn/clawback
	// from=holder, to=admin; set_admin from=old, to=new.
	From   string `json:"from,omitempty"`
	To     string `json:"to,omitempty"`
	Amount string `json:"amount,omitempty"`
}

// sep41Slots maps event names to their topic positions. Index 0 is the
// event name; 1 and 2 are the two address slots.
var sep41Slots = map[string][2]int{
	"transfer":  {1, 2},
	"mint":      {1, 2},
	"burn":      {2, 1},
	"clawback":  {2, 1},
	"set_admin": {1, 2},
}

// DetectTokenEvent reports the SEP-41 reading of an event, or nil when the
// event does not match the layout. Matching is structural: first topic a
// symbol naming a SEP-41 event, two address topics, and — for the
// amount-bearing events — an i128 value. A contract that happens to emit a
// transfer-shaped event without implementing SEP-41 is indistinguishable
// from one that does, and the annotation is honest either way: it describes
// the event's shape, not a verified interface claim.
func DetectTokenEvent(ev source.Event) *TokenInfo {
	// Topics and Value are raw decoded JSON; unmarshal into the generic
	// shape locally. An event whose topics are not a JSON array, or that
	// fails to unmarshal, is simply not a token event.
	var topics []any
	if err := json.Unmarshal(ev.Topics, &topics); err != nil {
		return nil
	}
	var value any
	if len(ev.Value) > 0 {
		if err := json.Unmarshal(ev.Value, &value); err != nil {
			return nil
		}
	}

	if len(topics) < 3 {
		return nil
	}
	slots, ok := sep41Slots[eventName(topics[0])]
	if !ok {
		return nil
	}
	from, okFrom := topicAddress(topics[slots[0]])
	to, okTo := topicAddress(topics[slots[1]])
	if !okFrom || !okTo {
		return nil
	}

	info := &TokenInfo{From: from, To: to}
	switch name := eventName(topics[0]); name {
	case "set_admin":
		info.Event = name
		return info
	default:
		info.Event = name
		if amount, ok := valueAmount(value); ok {
			info.Amount = amount
		}
		return info
	}
}

// eventName extracts the event name from the first topic, in either decoded
// shape: a bare string or the {"symbol": "..."} wrapper.
func eventName(topic any) string {
	switch v := topic.(type) {
	case string:
		return v
	case map[string]any:
		if s, ok := v["symbol"].(string); ok {
			return s
		}
	}
	return ""
}

// topicAddress extracts an address from a topic value, in either decoded
// shape: bare string or the {"address": "..."} wrapper.
func topicAddress(topic any) (string, bool) {
	switch v := topic.(type) {
	case string:
		return v, true
	case map[string]any:
		if s, ok := v["address"].(string); ok {
			return s, true
		}
	}
	return "", false
}

// valueAmount extracts the i128 amount from a decoded event value. Wide
// integers are decimal strings in the stored shape; small ones may arrive
// as JSON numbers, formatted back to a decimal string so the annotation is
// always a string a client can parse as a big integer.
func valueAmount(value any) (string, bool) {
	m, ok := value.(map[string]any)
	if !ok {
		return "", false
	}
	switch v := m["i128"].(type) {
	case string:
		return v, true
	case float64:
		return formatFloat(v), true
	}
	return "", false
}

func formatFloat(f float64) string {
	// Small i128s may arrive as JSON numbers; render back to decimal
	// without exponent, matching the string form.
	b, _ := json.Marshal(f)
	return string(b)
}

// annotatedEvent is an event response with the optional SEP-41 reading
// attached. Embedding keeps every existing field and its JSON shape; the
// "token" key appears only when the event matches the layout.
type annotatedEvent struct {
	source.Event
	Token *TokenInfo `json:"token,omitempty"`
}

// annotateEvents attaches the SEP-41 reading to each event that matches
// the layout. Non-token events pass through unchanged in shape.
func annotateEvents(events []source.Event) []annotatedEvent {
	out := make([]annotatedEvent, len(events))
	for i, ev := range events {
		out[i] = annotatedEvent{Event: ev, Token: DetectTokenEvent(ev)}
	}
	return out
}

// annotatedPage is an events page with annotated events. The next_cursor
// contract is unchanged.
type annotatedPage struct {
	Events     []annotatedEvent `json:"events"`
	NextCursor string           `json:"next_cursor,omitempty"`
}
