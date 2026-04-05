// ── Application state ─────────────────────────────────────────────────────────
//
// State initializes with empty defaults. init() in main.js populates the
// localStorage-dependent fields (prefs, favouriteSearches) and currentDate.

export const state = {
    channels:            [],
    programmes:          [],
    currentDate:         '',      // set by init() via getDateFromURL()
    prefs:               { hidden: {}, favourites: {} },  // set by init() via loadPrefs()
    activePage:          'guide', // current page: guide | search | favourites | settings
    nowLineTimer:        null,    // interval ID for the now-line updater
    categories:          [],      // cached from /api/categories
    searchResults:       [],      // current search results
    searchDebounce:      null,    // debounce timer ID
    selectedCategories:  new Set(), // currently selected category filters
    favouriteSearches:   [],      // set by init() via loadFavouriteSearches()
    favouriteResults:    {},      // map of favourite ID → search results
    favouriteResultsTime: 0,      // timestamp when favourites were last fetched
};
