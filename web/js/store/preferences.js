// ── Preferences store (localStorage) ─────────────────────────────────────────
//
// Manages channel preferences: hidden and favourite channel IDs.
// Stored in localStorage under the key 'tvguide-prefs'.
// Toggle functions only mutate state and persist — rendering is the caller's
// responsibility.

import { state } from '../state.js';

export function loadPrefs() {
    try {
        const raw = localStorage.getItem('tvguide-prefs');
        const p = raw ? JSON.parse(raw) : {};
        return { hidden: p.hidden || {}, favourites: p.favourites || {} };
    } catch {
        return { hidden: {}, favourites: {} };
    }
}

export function savePrefs() {
    localStorage.setItem('tvguide-prefs', JSON.stringify(state.prefs));
}

export function isHidden(channelId)    { return !!state.prefs.hidden[channelId]; }
export function isFavourite(channelId) { return !!state.prefs.favourites[channelId]; }

export function toggleHidden(channelId) {
    state.prefs.hidden[channelId] = !state.prefs.hidden[channelId];
    savePrefs();
}

export function toggleFavourite(channelId) {
    state.prefs.favourites[channelId] = !state.prefs.favourites[channelId];
    savePrefs();
}
