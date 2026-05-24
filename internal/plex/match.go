package plex

import (
	"strconv"
	"strings"

	"github.com/acbgbca/xmltvguide/internal/model"
)

// MatchSource identifies which property was used to pair a Plex entity with a
// local TV Guide entity. MatchSourceNone means no match was found (or the
// match was ambiguous).
type MatchSource int

const (
	MatchSourceNone MatchSource = iota
	MatchSourceID
	MatchSourceLCN
	MatchSourceName
	MatchSourceStartTime
)

// UnmatchedReason explains why an airing could not be paired with a local
// candidate. The poller logs this so that operators can investigate.
type UnmatchedReason int

const (
	UnmatchedNone UnmatchedReason = iota
	UnmatchedNoCandidate
	UnmatchedNoStartTimeMatch
)

// AiringMatchToleranceSeconds is the ±tolerance applied when matching a Plex
// grid entry to a local airing by start time. Live channels routinely run a
// few minutes late and shift their successor accordingly; the tolerance is
// large enough to absorb that drift but tight enough that successive shows
// don't overlap.
const AiringMatchToleranceSeconds = 300

// tierResult describes the outcome of a single matcher tier.
type tierResult int

const (
	tierMiss      tierResult = iota // no candidate matched — fall through to the next tier
	tierHit                         // exactly one candidate matched
	tierAmbiguous                   // two or more candidates matched — stop without falling through
)

// findUnique scans candidates and returns the sole element satisfying pred.
// The tierResult distinguishes "no match" (caller should fall through) from
// "ambiguous" (caller should stop with MatchSourceNone).
func findUnique(candidates []model.Channel, pred func(*model.Channel) bool) (*model.Channel, tierResult) {
	var match *model.Channel
	for i := range candidates {
		if !pred(&candidates[i]) {
			continue
		}
		if match != nil {
			return nil, tierAmbiguous
		}
		match = &candidates[i]
	}
	if match == nil {
		return nil, tierMiss
	}
	return match, tierHit
}

// MatchChannel pairs a Plex lineup channel with one local channel. The cascade
// is ID → LCN → Name. At each tier, an ambiguous result (two or more
// candidates match by the same property) returns MatchSourceNone without
// falling through — see issue #282 for the rationale. The ID tier is a no-op
// on Plex Cloud EPG (its `id` is a long hex string that won't collide with any
// local xmltv id) but remains useful for Plex XMLTV-provider users where the
// channel `id` is the xmltv id.
func MatchChannel(plex LineupChannel, candidates []model.Channel) (*model.Channel, MatchSource) {
	tiers := []struct {
		source MatchSource
		pred   func(*model.Channel) bool
	}{
		{MatchSourceID, idPredicate(plex)},
		{MatchSourceLCN, lcnPredicate(plex)},
		{MatchSourceName, namePredicate(plex)},
	}

	for _, t := range tiers {
		if t.pred == nil {
			continue
		}
		match, result := findUnique(candidates, t.pred)
		switch result {
		case tierHit:
			return match, t.source
		case tierAmbiguous:
			return nil, MatchSourceNone
		}
	}
	return nil, MatchSourceNone
}

// idPredicate returns a match function for the ID tier. Compares the Plex
// channel ID against the local channel ID; for Plex XMLTV-provider users this
// is the xmltv id, for Plex Cloud EPG users it is an opaque hex string that
// won't match (no-op).
func idPredicate(plex LineupChannel) func(*model.Channel) bool {
	if plex.ID == "" {
		return nil
	}
	return func(c *model.Channel) bool { return c.ID == plex.ID }
}

// lcnPredicate returns a match function for the LCN tier, or nil if Plex has
// no VCN or its VCN is not a valid integer. strconv.Atoi handles the
// zero-padded form ("001" → 1, "010" → 10).
func lcnPredicate(plex LineupChannel) func(*model.Channel) bool {
	if plex.VCN == "" {
		return nil
	}
	plexLCN, err := strconv.Atoi(plex.VCN)
	if err != nil {
		return nil
	}
	return func(c *model.Channel) bool {
		return c.LCN != nil && *c.LCN == plexLCN
	}
}

// namePredicate returns a match function for the case-insensitive display-name
// tier, or nil if Plex has no display name.
func namePredicate(plex LineupChannel) func(*model.Channel) bool {
	if plex.DisplayName == "" {
		return nil
	}
	target := strings.ToLower(plex.DisplayName)
	return func(c *model.Channel) bool {
		return strings.ToLower(c.DisplayName) == target
	}
}

// MatchAiring pairs a Plex grid entry with one airing on the already-matched
// channel using a ±AiringMatchToleranceSeconds window around the Plex
// BeginsAt; when multiple candidates fall inside the window the one with the
// smallest absolute delta wins. Plex Cloud EPG never emits dd_progid, so the
// older program-id tier is gone — see issue #287.
func MatchAiring(plex GridEntry, candidates []model.Airing) (*model.Airing, MatchSource, UnmatchedReason) {
	if len(candidates) == 0 {
		return nil, MatchSourceNone, UnmatchedNoCandidate
	}
	media, ok := plex.PrimaryMedia()
	if !ok {
		return nil, MatchSourceNone, UnmatchedNoStartTimeMatch
	}

	var best *model.Airing
	bestDelta := int64(AiringMatchToleranceSeconds + 1)
	for i := range candidates {
		delta := candidates[i].Start.Unix() - media.BeginsAt
		if delta < 0 {
			delta = -delta
		}
		if delta <= AiringMatchToleranceSeconds && delta < bestDelta {
			best = &candidates[i]
			bestDelta = delta
		}
	}
	if best == nil {
		return nil, MatchSourceNone, UnmatchedNoStartTimeMatch
	}
	return best, MatchSourceStartTime, UnmatchedNone
}
