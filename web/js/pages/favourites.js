import { state } from '../state.js';
import { logError } from '../api.js';
import { formatSearchDate } from '../utils/date.js';
import { isHidden } from '../store/preferences.js';
import { removeFavouriteSearch } from '../store/favourites.js';
import { openSearchAiringModal } from '../components/modal.js';
import { editFavouriteSearch } from './search.js';

export function renderFavouritesPage() {
    const list = document.getElementById('favouritesList');
    const loading = document.getElementById('favouritesLoading');
    const empty = document.getElementById('favouritesEmpty');

    list.innerHTML = '';

    if (state.favouriteSearches.length === 0) {
        loading.style.display = 'none';
        empty.style.display = '';
        return;
    }

    empty.style.display = 'none';

    // If results are fresh (< 5 min), reuse them
    const cacheAge = Date.now() - state.favouriteResultsTime;
    if (cacheAge < 5 * 60 * 1000 && Object.keys(state.favouriteResults).length > 0) {
        loading.style.display = 'none';
        renderFavouriteResults();
        return;
    }

    loading.style.display = '';
    executeFavouriteSearches();
}

async function executeFavouriteSearches() {
    const loading = document.getElementById('favouritesLoading');
    state.favouriteResults = {};

    const promises = state.favouriteSearches.map(async (fav) => {
        const params = new URLSearchParams({ q: fav.query });
        // Always exclude past on favourites page
        if (fav.mode === 'advanced') {
            params.set('mode', 'advanced');
            if (fav.categories && fav.categories.length > 0) {
                params.set('categories', fav.categories.join(','));
            }
        }
        if (fav.includeRepeats === false) {
            params.set('include_repeats', 'false');
        }

        try {
            const res = await fetch('/api/search?' + params);
            if (!res.ok) throw new Error(`/api/search returned ${res.status}`);
            const results = await res.json();
            state.favouriteResults[fav.id] = results;
            // Progressive rendering: update after each result
            renderFavouriteResults();
        } catch (err) {
            console.error(`Favourite search "${fav.name}" failed: ${err.message}`);
            logError({ type: 'explicit', message: err.message, stack: err?.stack, url: location.href });
            state.favouriteResults[fav.id] = { error: err.message };
        }
    });

    await Promise.all(promises);
    state.favouriteResultsTime = Date.now();
    loading.style.display = 'none';
    renderFavouriteResults();
}

function renderFavouriteResults() {
    const list = document.getElementById('favouritesList');
    list.innerHTML = '';

    for (const fav of state.favouriteSearches) {
        const groupEl = document.createElement('div');
        groupEl.className = 'fav-group';

        // Header: star + name + edit + delete
        const header = document.createElement('div');
        header.className = 'fav-group-header';

        const star = document.createElement('span');
        star.className = 'fav-group-star';
        star.textContent = '\u2605'; // ★
        header.appendChild(star);

        const nameEl = document.createElement('span');
        nameEl.className = 'fav-group-name';
        nameEl.textContent = '"' + fav.name + '"';
        header.appendChild(nameEl);

        const actions = document.createElement('div');
        actions.className = 'fav-group-actions';

        const editBtn = document.createElement('button');
        editBtn.className = 'fav-action-btn';
        editBtn.textContent = 'Edit';
        editBtn.title = 'Edit this search';
        editBtn.addEventListener('click', () => editFavouriteSearch(fav.id));
        actions.appendChild(editBtn);

        const deleteBtn = document.createElement('button');
        deleteBtn.className = 'fav-action-btn fav-delete-btn';
        deleteBtn.textContent = '\u2715'; // ✕
        deleteBtn.title = 'Remove favourite';
        deleteBtn.addEventListener('click', () => {
            if (confirm('Remove "' + fav.name + '" from favourites?')) {
                removeFavouriteSearch(fav.id);
                renderFavouritesPage();
            }
        });
        actions.appendChild(deleteBtn);

        header.appendChild(actions);
        groupEl.appendChild(header);

        // Results
        const results = state.favouriteResults[fav.id];
        if (results === undefined) {
            // Still loading
            const loadingEl = document.createElement('div');
            loadingEl.className = 'fav-loading';
            loadingEl.innerHTML = '<div class="loading-spinner fav-spinner"></div>';
            groupEl.appendChild(loadingEl);
        } else if (results && results.error) {
            // Failed
            const errorEl = document.createElement('div');
            errorEl.className = 'fav-no-results';
            errorEl.textContent = `Search failed: ${results.error}`;
            groupEl.appendChild(errorEl);
        } else {
            // Filter hidden channels
            const filtered = [];
            for (const group of results) {
                const visibleAirings = group.airings.filter(a => !isHidden(a.channelId));
                if (visibleAirings.length > 0) {
                    filtered.push({ title: group.title, airings: visibleAirings });
                }
            }

            if (filtered.length === 0) {
                const noResults = document.createElement('div');
                noResults.className = 'fav-no-results';
                noResults.textContent = 'No upcoming airings';
                groupEl.appendChild(noResults);
            } else {
                for (const titleGroup of filtered) {
                    const titleEl = document.createElement('div');
                    titleEl.className = 'fav-title-group';

                    const titleHeader = document.createElement('div');
                    titleHeader.className = 'fav-title-name';
                    titleHeader.textContent = titleGroup.title;
                    titleEl.appendChild(titleHeader);

                    for (const airing of titleGroup.airings) {
                        const airingEl = document.createElement('div');
                        airingEl.className = 'fav-airing';
                        airingEl.addEventListener('click', () => openSearchAiringModal(airing, titleGroup.title));

                        const channelEl = document.createElement('span');
                        channelEl.className = 'fav-airing-channel';
                        channelEl.textContent = airing.channelName || airing.channelId;
                        airingEl.appendChild(channelEl);

                        const timeEl = document.createElement('span');
                        timeEl.className = 'fav-airing-time';
                        timeEl.textContent = formatSearchDate(new Date(airing.startTime));
                        airingEl.appendChild(timeEl);

                        titleEl.appendChild(airingEl);
                    }

                    groupEl.appendChild(titleEl);
                }
            }
        }

        list.appendChild(groupEl);
    }
}
