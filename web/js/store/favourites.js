// ── Favourite searches store (localStorage) ───────────────────────────────────
//
// Manages saved search favourites.
// Stored in localStorage under the key 'tvguide-favourites'.

import { state } from '../state.js';

export function loadFavouriteSearches() {
    try {
        const raw = localStorage.getItem('tvguide-favourites');
        return raw ? JSON.parse(raw) : [];
    } catch {
        return [];
    }
}

export function saveFavouriteSearches() {
    localStorage.setItem('tvguide-favourites', JSON.stringify(state.favouriteSearches));
}

export function addFavouriteSearch(searchConfig) {
    const fav = {
        id: crypto.randomUUID(),
        name: searchConfig.query,
        query: searchConfig.query,
        mode: searchConfig.mode || 'simple',
    };
    if (fav.mode === 'advanced') {
        if (searchConfig.categories && searchConfig.categories.length > 0) {
            fav.categories = searchConfig.categories;
        }
        if (searchConfig.includePast) fav.includePast = true;
        if (searchConfig.includeRepeats === false) fav.includeRepeats = false;
    }
    state.favouriteSearches.push(fav);
    saveFavouriteSearches();
    return fav;
}

export function removeFavouriteSearch(id) {
    state.favouriteSearches = state.favouriteSearches.filter(f => f.id !== id);
    delete state.favouriteResults[id];
    saveFavouriteSearches();
}

export function findMatchingFavourite(query, mode, categories) {
    return state.favouriteSearches.find(f => {
        if (f.query !== query) return false;
        if (f.mode !== mode) return false;
        if (mode === 'advanced') {
            const favCats = (f.categories || []).slice().sort().join(',');
            const curCats = (categories || []).slice().sort().join(',');
            if (favCats !== curCats) return false;
        }
        return true;
    });
}
