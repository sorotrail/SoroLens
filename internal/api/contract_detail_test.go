package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/sorotrail/sorolens/internal/source"

	"github.com/stellar/go-stellar-sdk/strkey"
)

// contractID builds a valid contract strkey from a seed byte; handlers
// reject malformed IDs, so fixtures must be real strkeys.
func contractID(seed byte) string {
	raw := make([]byte, 32)
	for i := range raw {
		raw[i] = seed
	}
	s, err := strkey.Encode(strkey.VersionByteContract, raw)
	if err != nil {
		panic(err)
	}
	return s
}

func TestGetContract(t *testing.T) {
	id := contractID(0xA1)
	src := &fakeSource{contractStats: source.ContractStats{
		ContractID:  id,
		TotalEvents: 42,
		FirstLedger: 100,
		LastLedger:  200,
	}}
	srv := httptest.NewServer(New(src, discardLogger()).Routes())
	defer srv.Close()

	res, err := http.Get(srv.URL + "/contracts/" + id)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	var got source.ContractStats
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.ContractID != id || got.TotalEvents != 42 {
		t.Fatalf("got %+v", got)
	}
}

func TestGetContractMalformedID(t *testing.T) {
	srv := httptest.NewServer(New(&fakeSource{}, discardLogger()).Routes())
	defer srv.Close()

	res, err := http.Get(srv.URL + "/contracts/not-a-strkey")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("malformed ID = %d, want 400", res.StatusCode)
	}
}

func TestTxHashFilterPassedThrough(t *testing.T) {
	src := &fakeSource{}
	srv := httptest.NewServer(New(src, discardLogger()).Routes())
	defer srv.Close()

	res, err := http.Get(srv.URL + "/events?tx_hash=" + url.QueryEscape("abc123"))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if src.lastEventQuery.TxHash != "abc123" {
		t.Fatalf("tx_hash not parsed into the query: %+v", src.lastEventQuery)
	}
}
