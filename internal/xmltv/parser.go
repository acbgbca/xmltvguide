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

// Programme represents a single broadcast programme.
type Programme struct {
	Start      XmltvTime `xml:"start,attr"`
	Stop       XmltvTime `xml:"stop,attr"`
	Channel    string    `xml:"channel,attr"`
	Titles     []Name    `xml:"title"`
	SubTitles  []Name    `xml:"sub-title"`
	Descs      []Name    `xml:"desc"`
	Categories []Name    `xml:"category"`
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

// XMLTV timestamps use the format "YYYYMMDDHHmmss +HHMM".
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
