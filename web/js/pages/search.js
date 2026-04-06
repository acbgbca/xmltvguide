import { formatSearchDate } from '../utils/date.js';
import { state } from '../state.js';
import { fetchCategories, logError } from '../api.js';
import { isHidden } from '../store/preferences.js';
import { addFavouriteSearch, removeFavouriteSearch, findMatchingFavourite } from '../store/favourites.js';
import { openSearchAiringModal } from '../components/modal.js';
import { navigateToPage } from '../router.js';

// ── Search ───────────────────────────────────────────────────────────────────

export function getCurrentSearchConfig() {
    const q = document.getElementById('searchInput').value.trim();
    const useAdvanced = document.getElementById('searchDescriptions').checked;
    const includePast = document.getElementById('includePast').checked;
    const hideRepeats = document.getElementById('hideRepeats').checked;
    const categories = [...state.selectedCategories];

    const hasAdvanced = useAdvanced || categories.length > 0;
    return {
        query: q,
        mode: hasAdvanced ? 'advanced' : 'simple',
        categories: categories,
        includePast: includePast,
        includeRepeats: !hideRepeats,
    };
}

async function performSearch() {
    const input = document.getElementById('searchInput');
    const q = input.value.trim();

    if (q.length < 2) {
        state.searchResults = [];
        document.getElementById('searchResults').innerHTML = '';
        document.getElementById('searchHint').style.display = q.length > 0 ? '' : '';
        document.getElementById('searchHint').textContent = 'Enter at least 2 characters to search';
        return;
    }

    document.getElementById('searchHint').style.display = 'none';
    document.getElementById('searchSpinner').classList.add('visible');

    const params = new URLSearchParams({ q });
    const useAdvanced = document.getElementById('searchDescriptions').checked;
    const includePast = document.getElementById('includePast').checked;
    const hideRepeats = document.getElementById('hideRepeats').checked;

    if (useAdvanced) {
        params.set('mode', 'advanced');
    }
    if (includePast) {
        params.set('include_past', 'true');
    }
    if (hideRepeats) {
        params.set('include_repeats', 'false');
    }
    if (state.selectedCategories.size > 0) {
        // Category filtering requires advanced mode
        params.set('mode', 'advanced');
        params.set('categories', [...state.selectedCategories].join(','));
        if (!includePast) params.delete('include_past');
    }

    try {
        const res = await fetch('/api/search?' + params);
        if (!res.ok) throw new Error(`/api/search returned ${res.status}`);
        state.searchResults = await res.json();
        renderSearchResults();
    } catch (err) {
        console.error(`Search failed: ${err.message}`);
        logError({ type: 'explicit', message: err.message, stack: err?.stack, url: location.href });
        document.getElementById('searchResults').innerHTML =
            `<div class="search-empty">Search failed: ${err.message}</div>`;
    } finally {
        document.getElementById('searchSpinner').classList.remove('visible');
    }
}

export function triggerSearch() {
    clearTimeout(state.searchDebounce);
    state.searchDebounce = setTimeout(performSearch, 300);
}

function renderSearchResults() {
    const container = document.getElementById('searchResults');
    container.innerHTML = '';

    // Filter out hidden channels
    const filtered = [];
    for (const group of state.searchResults) {
        const visibleAirings = group.airings.filter(a => !isHidden(a.channelId));
        if (visibleAirings.length > 0) {
            filtered.push({ title: group.title, airings: visibleAirings });
        }
    }

    if (filtered.length === 0) {
        container.innerHTML = '<div class="search-empty">No programmes found</div>';
        return;
    }

    for (const group of filtered) {
        const groupEl = document.createElement('div');
        groupEl.className = 'search-group';

        // Title heading with favourite star
        const header = document.createElement('div');
        header.className = 'search-group-header';

        const titleEl = document.createElement('h3');
        titleEl.className = 'search-group-title';
        titleEl.textContent = group.title;
        header.appendChild(titleEl);

        const starBtn = document.createElement('button');
        starBtn.className = 'search-fav-btn';
        const config = getCurrentSearchConfig();
        const existingFav = findMatchingFavourite(config.query, config.mode, config.categories);
        starBtn.textContent = existingFav ? '\u2605' : '\u2606'; // ★ or ☆
        starBtn.classList.toggle('active', !!existingFav);
        starBtn.title = existingFav ? 'Remove from favourites' : 'Save as favourite';
        starBtn.addEventListener('click', (e) => {
            e.stopPropagation();
            const cfg = getCurrentSearchConfig();
            const match = findMatchingFavourite(cfg.query, cfg.mode, cfg.categories);
            if (match) {
                removeFavouriteSearch(match.id);
                starBtn.textContent = '\u2606';
                starBtn.classList.remove('active');
                starBtn.title = 'Save as favourite';
            } else {
                addFavouriteSearch(cfg);
                starBtn.textContent = '\u2605';
                starBtn.classList.add('active');
                starBtn.title = 'Remove from favourites';
            }
        });
        header.appendChild(starBtn);

        groupEl.appendChild(header);

        // Airings list
        for (const airing of group.airings) {
            const airingEl = document.createElement('div');
            airingEl.className = 'search-airing';
            airingEl.addEventListener('click', () => openSearchAiringModal(airing, group.title));

            const channelEl = document.createElement('span');
            channelEl.className = 'search-airing-channel';
            channelEl.textContent = airing.channelName || airing.channelId;
            airingEl.appendChild(channelEl);

            const timeEl = document.createElement('span');
            timeEl.className = 'search-airing-time';
            timeEl.textContent = formatSearchDate(new Date(airing.startTime));
            airingEl.appendChild(timeEl);

            if (airing.episodeNumDisplay || airing.subTitle) {
                const epEl = document.createElement('span');
                epEl.className = 'search-airing-episode';
                const parts = [];
                if (airing.episodeNumDisplay) parts.push(airing.episodeNumDisplay);
                if (airing.subTitle) parts.push(airing.subTitle);
                epEl.textContent = parts.join(' - ');
                airingEl.appendChild(epEl);
            }

            groupEl.appendChild(airingEl);
        }

        container.appendChild(groupEl);
    }
}

export function renderCategoryChips() {
    const container = document.getElementById('categoryChips');
    container.innerHTML = '';
    for (const cat of state.categories) {
        const chip = document.createElement('button');
        chip.className = 'category-chip' + (state.selectedCategories.has(cat) ? ' selected' : '');
        chip.textContent = cat;
        chip.addEventListener('click', () => {
            if (state.selectedCategories.has(cat)) {
                state.selectedCategories.delete(cat);
            } else {
                state.selectedCategories.add(cat);
            }
            chip.classList.toggle('selected');
            triggerSearch();
        });
        container.appendChild(chip);
    }
}

export function setupSearchPage() {
    const input = document.getElementById('searchInput');
    const clearBtn = document.getElementById('searchClear');
    const advToggle = document.getElementById('advancedToggle');
    const advPanel = document.getElementById('advancedOptions');

    input.addEventListener('input', triggerSearch);

    clearBtn.addEventListener('click', () => {
        input.value = '';
        state.searchResults = [];
        state.selectedCategories.clear();
        document.getElementById('searchResults').innerHTML = '';
        document.getElementById('searchHint').style.display = '';
        document.getElementById('searchHint').textContent = 'Enter at least 2 characters to search';
        renderCategoryChips();
    });

    advToggle.addEventListener('click', () => {
        const isOpen = advPanel.style.display !== 'none';
        advPanel.style.display = isOpen ? 'none' : '';
        advToggle.classList.toggle('open', !isOpen);
    });

    // Checkboxes re-trigger search
    document.getElementById('searchDescriptions').addEventListener('change', triggerSearch);
    document.getElementById('includePast').addEventListener('change', triggerSearch);
    document.getElementById('hideRepeats').addEventListener('change', triggerSearch);
}

export function loadSearchPageCategories() {
    return fetchCategories().then(renderCategoryChips).catch(err => {
        console.error('Failed to load categories:', err);
        logError({ type: 'explicit', message: err.message, stack: err?.stack, url: location.href });
    });
}

export function editFavouriteSearch(id) {
    const fav = state.favouriteSearches.find(f => f.id === id);
    if (!fav) return;

    // Navigate to search page
    navigateToPage('search');

    // Pre-fill search input
    document.getElementById('searchInput').value = fav.query;

    // Pre-fill advanced options
    const isAdvanced = fav.mode === 'advanced';
    document.getElementById('searchDescriptions').checked = isAdvanced;
    document.getElementById('includePast').checked = fav.includePast || false;
    document.getElementById('hideRepeats').checked = fav.includeRepeats === false;

    // Pre-fill categories
    state.selectedCategories.clear();
    if (fav.categories) {
        for (const cat of fav.categories) {
            state.selectedCategories.add(cat);
        }
    }

    // Show advanced panel if needed
    if (isAdvanced || (fav.categories && fav.categories.length > 0)) {
        document.getElementById('advancedOptions').style.display = '';
        document.getElementById('advancedToggle').classList.add('open');
    }

    // Render category chips and trigger search
    fetchCategories().then(renderCategoryChips).catch(err => console.warn('Failed to load categories:', err));
    triggerSearch();
}
