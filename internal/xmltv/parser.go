package xmltv

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"time"
)

// TV represents the root element of an XMLTV document.
type TV struct {
	Channels   []Channel   `xml:"channel"`
	Programmes []Programme `xml:"programme"`
}

// Channel represents a TV channel definition.
type Channel struct {
	ID           string `xml:"id,attr"`
	DisplayNames []Name `xml:"display-name"`
	Icons        []Icon `xml:"icon"`
}

// Programme represents a single broadcast slot.
type Programme struct {
	Start           XmltvTime        `xml:"start,attr"`
	Stop            XmltvTime        `xml:"stop,attr"`
	Channel         string           `xml:"channel,attr"`
	Titles          []Name           `xml:"title"`
	SubTitles       []Name           `xml:"sub-title"`
	Descs           []Name           `xml:"desc"`
	Categories      []Name           `xml:"category"`
	EpisodeNums     []EpisodeNum     `xml:"episode-num"`
	StarRatings     []StarRating     `xml:"star-rating"`
	Ratings         []Rating         `xml:"rating"`
	PreviouslyShown *PreviouslyShown `xml:"previously-shown"`
	Premiere        *Name            `xml:"premiere"`
	New             *EmptyElement    `xml:"new"`
	Date            string           `xml:"date"`
	Icons           []Icon           `xml:"icon"`
	Country         []Name           `xml:"country"`
}

// Name is a text element with an optional language attribute,
// used for titles, descriptions, categories, etc.
type Name struct {
	Value string `xml:",chardata"`
	Lang  string `xml:"lang,attr"`
}

// Icon holds a URL reference to a channel or programme image.
type Icon struct {
	Src string `xml:"src,attr"`
}

// EpisodeNum holds an episode number in a specific numbering system.
// Common systems: "xmltv_ns" (0-indexed season.episode.part),
// "onscreen" (human-readable e.g. "S02 E04"), "dd_progid" (TMS/Gracenote ID).
type EpisodeNum struct {
	Value  string `xml:",chardata"`
	System string `xml:"system,attr"`
}

// StarRating holds a star rating value (e.g. "4/5").
type StarRating struct {
	Value  string `xml:"value"`
	System string `xml:"system,attr"`
}

// Rating holds a content classification (e.g. "M", "PG", "MA15+").
type Rating struct {
	Value  string `xml:"value"`
	System string `xml:"system,attr"`
}

// PreviouslyShown indicates a repeat broadcast.
// Its presence on a Programme means the programme has aired before.
type PreviouslyShown struct {
	Start   string `xml:"start,attr"`
	Channel string `xml:"channel,attr"`
}

// EmptyElement is used for XML elements whose mere presence carries meaning
// (e.g. <new/> indicating a first-run programme).
type EmptyElement struct{}

// XmltvTime wraps time.Time to handle XMLTV's non-standard time format.
type XmltvTime struct {
	time.Time
}

// UnmarshalXMLAttr implements xml.UnmarshalerAttr for XMLTV timestamps.
func (t *XmltvTime) UnmarshalXMLAttr(attr xml.Attr) error {
	parsed, err := parseXmltvTime(attr.Value)
	if err != nil {
		return err
	}
	t.Time = parsed
	return nil
}

// XMLTV timestamps use the format "YYYYMMDDHHmmss ±HHMM".
// The timezone offset may or may not include a colon separator.
var xmltvLayouts = []string{
	"20060102150405 -0700",
	"20060102150405 -07:00",
	"20060102150405", // no timezone — treated as UTC by time.Parse
}

func parseXmltvTime(s string) (time.Time, error) {
	for _, layout := range xmltvLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse xmltv time: %q", s)
}

var httpClient = &http.Client{
	Timeout: 5 * time.Minute,
}

// Fetch downloads and parses an XMLTV document from the given URL.
func Fetch(url string) (*TV, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Accept", "text/xml, application/xml, */*")
	req.Header.Set("User-Agent", "xmltvguide/1.0")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned %s for %s", resp.Status, url)
	}

	return Parse(resp.Body)
}

// Parse reads and parses XMLTV data from r.
func Parse(r io.Reader) (*TV, error) {
	var tv TV
	if err := xml.NewDecoder(r).Decode(&tv); err != nil {
		return nil, fmt.Errorf("parsing xmltv: %w", err)
	}
	return &tv, nil
}
