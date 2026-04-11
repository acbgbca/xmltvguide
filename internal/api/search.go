package api

import (
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/acbgbca/xmltvguide/internal/model"
)

// searchResultAiring is the JSON shape for a single airing in a search result group.
type searchResultAiring struct {
	ChannelID         string    `json:"channelId"`
	ChannelName       string    `json:"channelName"`
	StartTime         time.Time `json:"startTime"`
	StopTime          time.Time `json:"stopTime"`
	SubTitle          string    `json:"subTitle,omitempty"`
	Description       string    `json:"description,omitempty"`
	EpisodeNumDisplay string    `json:"episodeNumDisplay,omitempty"`
	Categories        []string  `json:"categories,omitempty"`
	IsRepeat          bool      `json:"isRepeat"`
	IsPremiere        bool      `json:"isPremiere"`
}

// searchResultGroup is the JSON shape for a group of airings sharing the same title.
type searchResultGroup struct {
	Title   string               `json:"title"`
	Airings []searchResultAiring `json:"airings"`
}

func (h *Handler) getSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	isPremiere := r.URL.Query().Get("is_premiere") == "true"

	var categories []string
	if cats := r.URL.Query().Get("categories"); cats != "" {
		categories = strings.Split(cats, ",")
	}

	if q == "" && !isPremiere && len(categories) == 0 {
		http.Error(w, "at least one of q, is_premiere, or categories is required", http.StatusBadRequest)
		return
	}

	mode := r.URL.Query().Get("mode")
	includeRepeats := r.URL.Query().Get("include_repeats") != "false"
	today := r.URL.Query().Get("today") == "true"

	var results []model.SearchResult
	var err error

	if q == "" {
		// Browse mode: bypass FTS, query airings table directly.
		includePast := r.URL.Query().Get("include_past") == "true"
		results, err = h.db.SearchBrowse(categories, isPremiere, includePast, includeRepeats, today)
	} else if mode == "advanced" {
		includePast := r.URL.Query().Get("include_past") == "true"
		results, err = h.db.SearchAdvanced(q, categories, includePast, includeRepeats, today)
	} else {
		results, err = h.db.SearchSimple(q, includeRepeats, today)
	}

	if err != nil {
		http.Error(w, "search error", http.StatusInternalServerError)
		return
	}

	if r.URL.Query().Get("format") == "rss" {
		h.writeSearchRSS(w, r, q, mode, categories, results)
		return
	}

	// Group by title, tracking best rank per group.
	type groupData struct {
		airings  []searchResultAiring
		bestRank float64
	}
	groups := map[string]*groupData{}
	order := []string{} // preserve insertion order for determinism

	for _, sr := range results {
		a := searchResultAiring{
			ChannelID:         sr.ChannelID,
			ChannelName:       sr.ChannelName,
			StartTime:         sr.Start,
			StopTime:          sr.Stop,
			SubTitle:          sr.SubTitle,
			Description:       sr.Description,
			EpisodeNumDisplay: sr.EpisodeNumDisplay,
			Categories:        sr.Categories,
			IsRepeat:          sr.IsRepeat,
			IsPremiere:        sr.IsPremiere,
		}

		g, ok := groups[sr.Title]
		if !ok {
			g = &groupData{bestRank: math.MaxFloat64}
			groups[sr.Title] = g
			order = append(order, sr.Title)
		}
		g.airings = append(g.airings, a)
		if sr.Rank < g.bestRank {
			g.bestRank = sr.Rank
		}
	}

	// Sort airings within each group by start time ascending.
	for _, g := range groups {
		sort.Slice(g.airings, func(i, j int) bool {
			return g.airings[i].StartTime.Before(g.airings[j].StartTime)
		})
	}

	// Sort groups by best rank, then alphabetically by title.
	sort.SliceStable(order, func(i, j int) bool {
		ri, rj := groups[order[i]].bestRank, groups[order[j]].bestRank
		if ri != rj {
			return ri < rj
		}
		return order[i] < order[j]
	})

	response := make([]searchResultGroup, 0, len(order))
	for _, title := range order {
		response = append(response, searchResultGroup{
			Title:   title,
			Airings: groups[title].airings,
		})
	}

	writeJSON(w, response)
}
