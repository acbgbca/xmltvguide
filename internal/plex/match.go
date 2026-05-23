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
	MatchSourceProgID
	MatchSourceStartTime
)

// UnmatchedReason explains why an airing could not be paired with a local
// candidate. The poller logs this so that operators can investigate.
type UnmatchedReason int

const (
	UnmatchedNone UnmatchedReason = iota
	UnmatchedNoCandidate
	UnmatchedNoProgID
	UnmatchedProgIDMismatch
	UnmatchedNoStartTimeMatch
)

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
// falling through — see issue #282 for the rationale.
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

// idPredicate returns a match function for the ID tier, or nil if Plex has no
// xmltv id to compare against.
func idPredicate(plex LineupChannel) func(*model.Channel) bool {
	if plex.XMLTVID == "" {
		return nil
	}
	return func(c *model.Channel) bool { return c.ID == plex.XMLTVID }
}

// lcnPredicate returns a match function for the LCN tier, or nil if Plex has
// no LCN or its LCN is not a valid integer.
func lcnPredicate(plex LineupChannel) func(*model.Channel) bool {
	if plex.LCN == "" {
		return nil
	}
	plexLCN, err := strconv.Atoi(plex.LCN)
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
// channel. Callers must narrow candidates to the matched channel before
// invoking. See issue #282 for the strategy rules.
func MatchAiring(plex GridEntry, candidates []model.Airing) (*model.Airing, MatchSource, UnmatchedReason) {
	if len(candidates) == 0 {
		return nil, MatchSourceNone, UnmatchedNoCandidate
	}

	if plex.DdProgID != "" {
		for i := range candidates {
			if candidates[i].ProgID == plex.DdProgID {
				return &candidates[i], MatchSourceProgID, UnmatchedNone
			}
		}
		return nil, MatchSourceNone, UnmatchedProgIDMismatch
	}

	for i := range candidates {
		if candidates[i].Start.Unix() == plex.BeginsAt {
			return &candidates[i], MatchSourceStartTime, UnmatchedNone
		}
	}
	return nil, MatchSourceNone, UnmatchedNoStartTimeMatch
}
