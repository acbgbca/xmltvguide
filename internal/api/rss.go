package api

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/acbgbca/xmltvguide/internal/model"
)

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
	Title       string      `xml:"title"`
	Description xmlCDATA    `xml:"description"`
	PubDate     string      `xml:"pubDate"`
	GUID        xmlRSSGUID  `xml:"guid"`
	Categories  []string    `xml:"category"`
	Enclosure   *xmlRSSEncl `xml:"enclosure,omitempty"`
	Source      string      `xml:"source"`
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
	fmt.Fprintf(&b, "<p><strong>Channel:</strong> %s</p>\n", sr.ChannelName)

	timeFmt := "Mon 2 Jan 3:04 PM"
	fmt.Fprintf(&b, "<p><strong>Time:</strong> %s – %s</p>\n",
		sr.Start.Local().Format(timeFmt), sr.Stop.Local().Format(timeFmt))

	if sr.Description != "" {
		fmt.Fprintf(&b, "<p>%s</p>\n", sr.Description)
	}
	if sr.EpisodeNumDisplay != "" {
		fmt.Fprintf(&b, "<p><strong>Episode:</strong> %s</p>\n", sr.EpisodeNumDisplay)
	}
	if sr.StarRating != "" {
		fmt.Fprintf(&b, "<p><strong>Rating:</strong> %s</p>\n", sr.StarRating)
	}
	if sr.ContentRating != "" {
		fmt.Fprintf(&b, "<p><strong>Classification:</strong> %s</p>\n", sr.ContentRating)
	}
	if sr.Year != "" {
		fmt.Fprintf(&b, "<p><strong>Year:</strong> %s</p>\n", sr.Year)
	}
	if sr.Country != "" {
		fmt.Fprintf(&b, "<p><strong>Country:</strong> %s</p>\n", sr.Country)
	}
	if len(sr.Categories) > 0 {
		fmt.Fprintf(&b, "<p><strong>Categories:</strong> %s</p>\n", strings.Join(sr.Categories, ", "))
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
