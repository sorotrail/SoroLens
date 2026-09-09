package web

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/sorotrail/sorolens/internal/config"
	"github.com/sorotrail/sorolens/internal/source"
)

// recentEventsOnIndex is how many events the overview page shows.
const recentEventsOnIndex = 25

// indexData backs the overview page.
type indexData struct {
	Stats source.Stats
	// RecentFragment reuses the shared events table, so the overview and the
	// contract page render events identically.
	RecentFragment eventsFragment
}

func (s *Server) index(w http.ResponseWriter, r *http.Request) {
	stats, err := s.src.Stats(r.Context())
	if err != nil {
		s.fail(w, r, "loading stats", err)
		return
	}

	page, err := s.src.ListEvents(r.Context(), source.EventQuery{Limit: recentEventsOnIndex})
	if err != nil {
		s.fail(w, r, "loading recent events", err)
		return
	}

	fragment := eventsFragment{Events: page.Events}
	if page.NextCursor != "" {
		next := url.Values{}
		next.Set("limit", strconv.Itoa(recentEventsOnIndex))
		next.Set("cursor", page.NextCursor)
		fragment.MoreURL = "/partials/events?" + next.Encode()
	}

	s.render(w, r, "index", "Overview", indexData{Stats: stats, RecentFragment: fragment})
}

// contractsData backs the contracts list page.
type contractsData struct {
	Contracts  []source.Contract
	Search     string
	NextCursor string
	// NextURL is the ready-made link to the following page, so the template
	// does not have to rebuild the query string.
	NextURL string
}

func (s *Server) contracts(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	q := source.ContractQuery{
		Search: strings.TrimSpace(params.Get("search")),
		Cursor: params.Get("cursor"),
	}

	page, err := s.src.ListContracts(r.Context(), q)
	if err != nil {
		s.fail(w, r, "listing contracts", err)
		return
	}

	data := contractsData{
		Contracts:  page.Contracts,
		Search:     q.Search,
		NextCursor: page.NextCursor,
	}
	if page.NextCursor != "" {
		next := url.Values{}
		if q.Search != "" {
			next.Set("search", q.Search)
		}
		next.Set("cursor", page.NextCursor)
		data.NextURL = "/contracts?" + next.Encode()
	}

	s.render(w, r, "contracts", "Contracts", data)
}

// contractData backs the contract detail page.
type contractData struct {
	ContractID string
	Stats      source.ContractStats
	Events     eventsFragment
}

func (s *Server) contract(w http.ResponseWriter, r *http.Request) {
	contractID := strings.ToUpper(strings.TrimSpace(chi.URLParam(r, "id")))
	if !config.ValidContractID(contractID) {
		w.WriteHeader(http.StatusBadRequest)
		s.render(w, r, "notfound", "Invalid contract", map[string]string{
			"Heading": "Invalid contract ID",
			"Message": "Contract IDs start with C and are 56 characters long.",
		})
		return
	}

	stats, err := s.src.ContractStats(r.Context(), contractID)
	if err != nil {
		s.fail(w, r, "loading contract stats", err)
		return
	}

	fragment, err := s.eventsFragmentFor(r, contractID)
	if err != nil {
		s.badRequest(w, r, err.Error())
		return
	}

	s.render(w, r, "contract", "Contract "+contractID, contractData{
		ContractID: contractID,
		Stats:      stats,
		Events:     fragment,
	})
}

func (s *Server) event(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	event, err := s.src.GetEvent(r.Context(), id)
	if isNotFound(err) {
		w.WriteHeader(http.StatusNotFound)
		s.render(w, r, "notfound", "Event not found", map[string]string{
			"Heading": "Event not found",
			"Message": "No event with ID " + id + " is available from this source.",
		})
		return
	}
	if err != nil {
		s.fail(w, r, "loading event", err)
		return
	}

	s.render(w, r, "event", "Event "+event.ID, event)
}

// search routes a query to whichever page can answer it.
func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))

	switch classifySearch(query) {
	case searchContract:
		http.Redirect(w, r, "/contracts/"+strings.ToUpper(query), http.StatusSeeOther)
	case searchEvent:
		http.Redirect(w, r, "/events/"+url.PathEscape(query), http.StatusSeeOther)
	default:
		// Not a recognizable ID, so fall back to a substring search over the
		// contracts list, which is the useful interpretation of a partial ID.
		if query == "" {
			http.Redirect(w, r, "/contracts", http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, "/contracts?search="+url.QueryEscape(query), http.StatusSeeOther)
	}
}

// partialEvents serves the events table on its own, for htmx to swap in.
func (s *Server) partialEvents(w http.ResponseWriter, r *http.Request) {
	contractID := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("contract_id")))
	if contractID != "" && !config.ValidContractID(contractID) {
		s.badRequest(w, r, "invalid contract ID")
		return
	}

	fragment, err := s.eventsFragmentFor(r, contractID)
	if err != nil {
		s.badRequest(w, r, err.Error())
		return
	}
	s.renderPartial(w, "events-table", fragment)
}

// eventsFragment is everything the events table partial needs to render itself
// and its own "load more" control.
type eventsFragment struct {
	Events     []source.Event
	ContractID string
	// Filters echoes the active filter values back into the form.
	Filters filterValues
	// MoreURL is the htmx URL for the next page, empty when exhausted.
	MoreURL string
}

// filterValues are the raw filter inputs, kept as strings so the form can
// redisplay exactly what was typed.
type filterValues struct {
	Type       string
	Topic      string
	FromLedger string
	ToLedger   string
}

// eventsFragmentFor parses the filters from the request and loads one page of
// events. Both the full contract page and the htmx partial use it, so filters
// behave the same either way.
func (s *Server) eventsFragmentFor(r *http.Request, contractID string) (eventsFragment, error) {
	q, err := eventQueryFromRequest(r)
	if err != nil {
		return eventsFragment{}, err
	}
	q.ContractID = contractID

	page, err := s.src.ListEvents(r.Context(), q)
	if err != nil {
		return eventsFragment{}, err
	}

	params := r.URL.Query()
	fragment := eventsFragment{
		Events:     page.Events,
		ContractID: contractID,
		Filters: filterValues{
			Type:       params.Get("type"),
			Topic:      params.Get("topic"),
			FromLedger: params.Get("from_ledger"),
			ToLedger:   params.Get("to_ledger"),
		},
	}

	if page.NextCursor != "" {
		next := url.Values{}
		if contractID != "" {
			next.Set("contract_id", contractID)
		}
		for _, key := range []string{"type", "topic", "from_ledger", "to_ledger", "limit"} {
			if v := params.Get(key); v != "" {
				next.Set(key, v)
			}
		}
		next.Set("cursor", page.NextCursor)
		fragment.MoreURL = "/partials/events?" + next.Encode()
	}

	return fragment, nil
}

func (s *Server) badRequest(w http.ResponseWriter, r *http.Request, message string) {
	w.WriteHeader(http.StatusBadRequest)
	s.render(w, r, "notfound", "Bad request", map[string]string{
		"Heading": "Bad request",
		"Message": message,
	})
}
