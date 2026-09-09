package decode

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRender(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"symbol", `{"symbol":"transfer"}`, "transfer"},
		{"string", `{"string":"hello world"}`, "hello world"},
		{"address", `{"address":"GABC"}`, "GABC"},
		{"bool", `{"bool":true}`, "true"},
		{"void", `{"void":null}`, "void"},
		{"u32 number", `{"u32":42}`, "42"},
		{"i128 decimal string", `{"i128":"1000000000000"}`, "1000000000000"},
		{"negative i128", `{"i128":"-1"}`, "-1"},
		{"bytes get a hex prefix", `{"bytes":"deadbeef"}`, "0xdeadbeef"},
		{"empty vec", `{"vec":[]}`, "[]"},
		{
			name: "vec of wrapped values",
			in:   `{"vec":[{"symbol":"a"},{"u32":1}]}`,
			want: "[a, 1]",
		},
		{
			name: "map renders as key: value",
			in:   `{"map":[{"key":{"symbol":"amount"},"val":{"i128":"5"}}]}`,
			want: "{amount: 5}",
		},
		{
			name: "nested vec inside map",
			in:   `{"map":[{"key":{"symbol":"ids"},"val":{"vec":[{"u32":1},{"u32":2}]}}]}`,
			want: "{ids: [1, 2]}",
		},
		{
			name: "unknown wrapper falls back to its content",
			in:   `{"unknown":"ScvError(...)"}`,
			want: "ScvError(...)",
		},
		{"json null", `null`, "null"},
		{"bare string", `"plain"`, "plain"},
		{"bare number", `7`, "7"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, Render(json.RawMessage(tt.in)))
		})
	}
}

func TestRenderDegradesGracefully(t *testing.T) {
	// Rendering is display-only, so malformed input must show something rather
	// than fail: the raw text is more useful to a reader than an error.
	t.Run("empty input", func(t *testing.T) {
		assert.Equal(t, "", Render(nil))
	})

	t.Run("invalid json returns the raw text", func(t *testing.T) {
		assert.Equal(t, "{not json", Render(json.RawMessage("{not json")))
	})

	t.Run("unrecognized object is rendered sorted", func(t *testing.T) {
		// Two keys, so it is not a wrapper; keys sort for stable output.
		assert.Equal(t, "{a: 1, b: 2}", Render(json.RawMessage(`{"b":2,"a":1}`)),
			"expected keys sorted regardless of input order")
	})
}

func TestRenderRespectsDepthLimit(t *testing.T) {
	// Build a value nested past MaxRenderDepth and confirm rendering
	// terminates rather than recursing without bound.
	nested := `{"u32":1}`
	for i := 0; i < MaxRenderDepth+5; i++ {
		nested = `{"vec":[` + nested + `]}`
	}

	got := Render(json.RawMessage(nested))
	assert.Contains(t, got, "…", "expected the depth limit to truncate")
}

func TestRenderTopics(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{
			name: "typical transfer topics",
			in:   `[{"symbol":"transfer"},{"address":"GABC"},{"address":"GDEF"}]`,
			want: []string{"transfer", "GABC", "GDEF"},
		},
		{"empty array", `[]`, []string{}},
		{"nil input", ``, nil},
		{"not an array falls back to raw", `{"symbol":"x"}`, []string{`{"symbol":"x"}`}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RenderTopics(json.RawMessage(tt.in))
			if tt.want == nil {
				assert.Nil(t, got)
				return
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestEventName(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "first topic symbol is the event name",
			in:   `[{"symbol":"transfer"},{"address":"GABC"}]`,
			want: "transfer",
		},
		{
			name: "a string first topic also counts",
			in:   `[{"string":"mint"}]`,
			want: "mint",
		},
		{
			name: "a non-symbol first topic has no name",
			in:   `[{"u32":1},{"symbol":"transfer"}]`,
			want: "",
		},
		{"empty topics", `[]`, ""},
		{"nil topics", ``, ""},
		{"malformed topics", `not json`, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, EventName(json.RawMessage(tt.in)))
		})
	}
}

func TestShorten(t *testing.T) {
	const contract = "CDLZFC3SYJYDZT7K67VZ75HPJVIEUVNIXF47ZG2FB2RMQQVU2HHGCYSC"

	tests := []struct {
		name string
		in   string
		keep int
		want string
	}{
		{"long id is abbreviated", contract, 6, "CDLZFC…HGCYSC"},
		{"short strings pass through", "abc", 6, "abc"},
		{"zero keep passes through", contract, 0, contract},
		{"negative keep passes through", contract, -1, contract},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, Shorten(tt.in, tt.keep))
		})
	}
}

func TestTopicsValuePrefersNodeDecodedJSON(t *testing.T) {
	// When the node supports xdrFormat "json" there is nothing to decode
	// locally, and the decoder must not be consulted at all.
	topics, value, err := TopicsValue(failingDecoder{}, RawEvent{
		TopicJSON: []json.RawMessage{json.RawMessage(`{"symbol":"transfer"}`)},
		ValueJSON: json.RawMessage(`{"i128":"5"}`),
	})
	require.NoError(t, err)
	assert.JSONEq(t, `[{"symbol":"transfer"}]`, string(topics))
	assert.JSONEq(t, `{"i128":"5"}`, string(value))
}

func TestTopicsValueFallsBackToXDR(t *testing.T) {
	topics, value, err := TopicsValue(stubDecoder{}, RawEvent{
		Topic: []string{"topic-a", "topic-b"},
		Value: "the-value",
	})
	require.NoError(t, err)
	assert.JSONEq(t, `[{"symbol":"topic-a"},{"symbol":"topic-b"}]`, string(topics))
	assert.JSONEq(t, `{"symbol":"the-value"}`, string(value))
}

func TestTopicsValueWithNoValue(t *testing.T) {
	topics, value, err := TopicsValue(stubDecoder{}, RawEvent{})
	require.NoError(t, err)
	assert.JSONEq(t, `[]`, string(topics))
	assert.Equal(t, "null", string(value))
}

func TestTopicsValuePropagatesDecoderErrors(t *testing.T) {
	_, _, err := TopicsValue(failingDecoder{}, RawEvent{Topic: []string{"bad"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decoding topic 0")
}

// stubDecoder wraps whatever it is given as a symbol, so tests can assert on
// which path produced a value without depending on real XDR.
type stubDecoder struct{}

func (stubDecoder) DecodeScVal(s string) (json.RawMessage, error) {
	return json.Marshal(map[string]string{"symbol": s})
}

// failingDecoder fails on every call, proving a path never reaches it.
type failingDecoder struct{}

func (failingDecoder) DecodeScVal(string) (json.RawMessage, error) {
	return nil, assert.AnError
}
