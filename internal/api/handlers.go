package api

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"math"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/acbgbca/xmltvguide/internal/model"
)

// store is the narrow interface the Handler needs from the database layer.
type store interface {
	GetChannels() ([]model.Channel, error)
	GetAirings(date time.Time) ([]model.Airing, error)
	GetStatus() model.Status
	EnsureChannelIcon(ctx context.Context, channelID string) (string, error)
	SearchSimple(query string, includeRepeats bool, today bool) ([]model.SearchResult, error)
	SearchAdvanced(query string, categories []string, includePast bool, includeRepeats bool, today bool) ([]model.SearchResult, error)
	GetCategories() ([]string, error)
}

// Handler holds the HTTP handler dependencies.
type Handler struct {
	db     store
	rssTTL int // default RSS TTL in minutes; 0 means use hard-coded default (360)
}

// New creates a new Handler backed by db. rssTTL is the server-wide default
// RSS feed TTL in minutes (from the RSS_TTL env var); pass 0 to use the
// hard-coded default of 360 minutes.
func New(db store, rssTTL int) *Handler {
	return &Handler{db: db, rssTTL: rssTTL}
}

// RegisterRoutes adds all API routes to mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/channels", h.getChannels)
	mux.HandleFunc("GET /api/guide", h.getGuide)
	mux.HandleFunc("GET /api/status", h.getStatus)
	mux.HandleFunc("GET /api/search", h.getSearch)
	mux.HandleFunc("GET /api/categories", h.getCategories)
	mux.HandleFunc("GET /images/channel/{id}", h.serveChannelIcon)
}

func (h *Handler) getChannels(w http.ResponseWriter, r *http.Request) {
	channels, err := h.db.GetChannels()
	if err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, channels)
}

func (h *Handler) getGuide(w http.ResponseWriter, r *http.Request) {
	dateStr := r.URL.Query().Get("date")
	var date time.Time
	if dateStr == "" {
		date = time.Now()
	} else {
		var err error
		date, err = time.ParseInLocation("2006-01-02", dateStr, time.Local)
		if err != nil {
			http.Error(w, "invalid date, expected YYYY-MM-DD", http.StatusBadRequest)
			return
		}
	}
	airings, err := h.db.GetAirings(date)
	if err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, airings)
}

func (h *Handler) getStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, h.db.GetStatus())
}

func (h *Handler) serveChannelIcon(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	localPath, err := h.db.EnsureChannelIcon(r.Context(), id)
	if err != nil {
		http.Error(w, "failed to retrieve icon", http.StatusInternalServerError)
		return
	}
	if localPath == "" {
		http.NotFound(w, r)
		return
	}

	data, err := os.ReadFile(localPath)
	if err != nil {
		http.Error(w, "failed to read icon", http.StatusInternalServerError)
		return
	}

	contentType := mime.TypeByExtension(filepath.Ext(localPath))
	if contentType == "" {
		contentType = http.DetectContentType(data)
	}
	w.Header().Set("Content-Type", contentType)
	w.Write(data) //nolint:errcheck
}

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
	if q == "" {
		http.Error(w, "missing required parameter: q", http.StatusBadRequest)
		return
	}

	mode := r.URL.Query().Get("mode")
	includeRepeats := r.URL.Query().Get("include_repeats") != "false"
	today := r.URL.Query().Get("today") == "true"

	var results []model.SearchResult
	var err error

	var categories []string
	if mode == "advanced" {
		if cats := r.URL.Query().Get("categories"); cats != "" {
			categories = strings.Split(cats, ",")
		}
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

// --- RSS output ---

const defaultRSSTTL = 360

type xmlRSS struct {
	XMLName xml.Name      `xml:"rss"`
	Version string        `xml:"version,attr"`
	Channel xmlRSSChannel `xml:"channel"`
}

type xmlRSSChannel struct {
	Title         string       `xml:"title"`
	Description   string       `xml:"description"`
	LastBuildDate string       `xml:"lastBuildDate"`
	TTL           int          `xml:"ttl"`
	Items         []xmlRSSItem `xml:"item"`
}

type xmlRSSItem struct {
	Title       string          `xml:"title"`
	Description xmlCDATA        `xml:"description"`
	PubDate     string          `xml:"pubDate"`
	GUID        xmlRSSGUID      `xml:"guid"`
	Categories  []string        `xml:"category"`
	Enclosure   *xmlRSSEncl     `xml:"enclosure,omitempty"`
	Source      string          `xml:"source"`
}

type xmlRSSGUID struct {
	IsPermaLink string `xml:"isPermaLink,attr"`
	Value       string `xml:",chardata"`
}

type xmlRSSEncl struct {
	URL  string `xml:"url,attr"`
	Type string `xml:"type,attr"`
}

// xmlCDATA wraps a string so it is marshalled as a CDATA section.
type xmlCDATA struct {
	Value string `xml:",cdata"`
}

func (h *Handler) writeSearchRSS(w http.ResponseWriter, r *http.Request, query, mode string, categories []string, results []model.SearchResult) {
	// Sort results by start time ascending.
	sort.Slice(results, func(i, j int) bool {
		return results[i].Start.Before(results[j].Start)
	})

	// Resolve TTL: query param > env var default > hard-coded.
	ttl := defaultRSSTTL
	if h.rssTTL > 0 {
		ttl = h.rssTTL
	}
	if v := r.URL.Query().Get("ttl"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			ttl = n
		}
	}

	// Build channel description.
	desc := fmt.Sprintf("Search results for %q (mode: %s)", query, mode)
	if mode == "" {
		desc = fmt.Sprintf("Search results for %q (mode: simple)", query)
	}
	if len(categories) > 0 {
		desc += fmt.Sprintf(", categories: %s", strings.Join(categories, ", "))
	}

	items := make([]xmlRSSItem, 0, len(results))
	for _, sr := range results {
		items = append(items, buildRSSItem(sr))
	}

	rss := xmlRSS{
		Version: "2.0",
		Channel: xmlRSSChannel{
			Title:         "TV Guide Search: " + query,
			Description:   desc,
			LastBuildDate: time.Now().Format(time.RFC1123Z),
			TTL:           ttl,
			Items:         items,
		},
	}

	w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
	w.Write([]byte(xml.Header)) //nolint:errcheck
	if err := xml.NewEncoder(w).Encode(rss); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

func buildRSSItem(sr model.SearchResult) xmlRSSItem {
	// Build title: "Title - SubTitle (EpisodeNumDisplay)"
	title := sr.Title
	if sr.SubTitle != "" {
		title += " - " + sr.SubTitle
	}
	if sr.EpisodeNumDisplay != "" {
		title += " (" + sr.EpisodeNumDisplay + ")"
	}

	// Build description HTML.
	var b strings.Builder
	b.WriteString(fmt.Sprintf("<p><strong>Channel:</strong> %s</p>\n", sr.ChannelName))

	timeFmt := "Mon 2 Jan 3:04 PM"
	b.WriteString(fmt.Sprintf("<p><strong>Time:</strong> %s – %s</p>\n",
		sr.Start.Local().Format(timeFmt), sr.Stop.Local().Format(timeFmt)))

	if sr.Description != "" {
		b.WriteString(fmt.Sprintf("<p>%s</p>\n", sr.Description))
	}
	if sr.EpisodeNumDisplay != "" {
		b.WriteString(fmt.Sprintf("<p><strong>Episode:</strong> %s</p>\n", sr.EpisodeNumDisplay))
	}
	if sr.StarRating != "" {
		b.WriteString(fmt.Sprintf("<p><strong>Rating:</strong> %s</p>\n", sr.StarRating))
	}
	if sr.ContentRating != "" {
		b.WriteString(fmt.Sprintf("<p><strong>Classification:</strong> %s</p>\n", sr.ContentRating))
	}
	if sr.Year != "" {
		b.WriteString(fmt.Sprintf("<p><strong>Year:</strong> %s</p>\n", sr.Year))
	}
	if sr.Country != "" {
		b.WriteString(fmt.Sprintf("<p><strong>Country:</strong> %s</p>\n", sr.Country))
	}
	if len(sr.Categories) > 0 {
		b.WriteString(fmt.Sprintf("<p><strong>Categories:</strong> %s</p>\n", strings.Join(sr.Categories, ", ")))
	}
	if sr.IsPremiere {
		b.WriteString("<p><em>Premiere</em></p>\n")
	}
	if sr.IsRepeat {
		b.WriteString("<p><em>Repeat</em></p>\n")
	}

	item := xmlRSSItem{
		Title:       title,
		Description: xmlCDATA{Value: b.String()},
		PubDate:     sr.Start.Format(time.RFC1123Z),
		GUID: xmlRSSGUID{
			IsPermaLink: "false",
			Value:       sr.ChannelID + "/" + sr.Start.Format(time.RFC3339),
		},
		Categories: sr.Categories,
		Source:     sr.ChannelName,
	}

	if sr.Icon != "" {
		item.Enclosure = &xmlRSSEncl{
			URL:  sr.Icon,
			Type: "image/jpeg",
		}
	}

	return item
}

func (h *Handler) getCategories(w http.ResponseWriter, r *http.Request) {
	cats, err := h.db.GetCategories()
	if err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, cats)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}
