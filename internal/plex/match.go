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

// MatchChannel pairs a Plex lineup channel with one local channel. The cascade
// is ID → LCN → Name. At each tier, an ambiguous result (two or more
// candidates match by the same property) returns MatchSourceNone without
// falling through — see issue #282 for the rationale.
func MatchChannel(plex LineupChannel, candidates []model.Channel) (*model.Channel, MatchSource) {
	if len(candidates) == 0 {
		return nil, MatchSourceNone
	}

	// Tier 1: exact xmltv id equality.
	if plex.XMLTVID != "" {
		var hits []*model.Channel
		for i := range candidates {
			if candidates[i].ID == plex.XMLTVID {
				hits = append(hits, &candidates[i])
			}
		}
		if len(hits) == 1 {
			return hits[0], MatchSourceID
		}
		if len(hits) > 1 {
			return nil, MatchSourceNone
		}
	}

	// Tier 2: exact LCN equality (only when both sides have an LCN).
	if plex.LCN != "" {
		if plexLCN, err := strconv.Atoi(plex.LCN); err == nil {
			var hits []*model.Channel
			for i := range candidates {
				if candidates[i].LCN != nil && *candidates[i].LCN == plexLCN {
					hits = append(hits, &candidates[i])
				}
			}
			if len(hits) == 1 {
				return hits[0], MatchSourceLCN
			}
			if len(hits) > 1 {
				return nil, MatchSourceNone
			}
		}
	}

	// Tier 3: case-insensitive display_name equality.
	if plex.DisplayName != "" {
		target := strings.ToLower(plex.DisplayName)
		var hits []*model.Channel
		for i := range candidates {
			if strings.ToLower(candidates[i].DisplayName) == target {
				hits = append(hits, &candidates[i])
			}
		}
		if len(hits) == 1 {
			return hits[0], MatchSourceName
		}
		if len(hits) > 1 {
			return nil, MatchSourceNone
		}
	}

	return nil, MatchSourceNone
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
