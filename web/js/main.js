import { getTodayString, addDays, formatDateLong, formatSearchDate } from './utils/date.js';
import { state } from './state.js';
import { fetchChannels, fetchGuide, fetchCategories, logError } from './api.js';
import { loadPrefs, isHidden, isFavourite, toggleHidden, toggleFavourite } from './store/preferences.js';
import { loadFavouriteSearches, addFavouriteSearch, removeFavouriteSearch, findMatchingFavourite } from './store/favourites.js';
import { openSearchAiringModal, closeModal } from './components/modal.js';
import {
    renderGuide, renderTimeAxis, setupScrollSync, scrollToNow,
    startNowLineTimer, stopNowLineTimer,
    navigateToDate, hasAiringsStartingOn,
    getDateFromURL,
} from './pages/guide.js';

window.onerror = (message, source, lineno, colno, error) => {
    logError({ type: 'onerror', message: String(message), source, lineno, colno,
                stack: error?.stack, url: location.href });
};

window.addEventListener('unhandledrejection', event => {
    const err = event.reason;
    logError({ type: 'unhandledrejection',
                message: err instanceof Error ? err.message : String(err),
                stack: err?.stack, url: location.href });
});

function showError(message) {
    console.error(message);
}

function getCurrentSearchConfig() {
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

function editFavouriteSearch(id) {
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

// ── Routing ──────────────────────────────────────────────────────────────────

const PAGES = ['guide', 'search', 'favourites', 'settings'];

function getPageFromPath() {
    const path = window.location.pathname.replace(/^\/+/, '').split('?')[0];
    if (PAGES.includes(path)) return path;
    return 'guide';
}

function navigateToPage(page, { pushState = true } = {}) {
    if (!PAGES.includes(page)) page = 'guide';
    state.activePage = page;

    // Update URL
    if (pushState) {
        const url = page === 'guide' ? '/' + (window.location.search || '') : '/' + page;
        history.pushState({}, '', url);
    }

    // Show/hide pages
    for (const p of PAGES) {
        const el = document.getElementById('page-' + p);
        if (el) el.style.display = p === page ? '' : 'none';
    }

    // Update bottom nav active state
    document.querySelectorAll('.bottom-nav-btn').forEach(btn => {
        btn.classList.toggle('active', btn.dataset.page === page);
    });

    // Top bar is only visible on the Guide tab
    document.getElementById('topBar').style.display = page === 'guide' ? '' : 'none';

    // Start/stop the now-line timer based on whether Guide is active
    if (page === 'guide') {
        startNowLineTimer();
    } else {
        stopNowLineTimer();
    }

    // Render settings when switching to that page
    if (page === 'settings') {
        renderSettingsPanel();
    }

    // Load categories when entering search page
    if (page === 'search') {
        fetchCategories().then(renderCategoryChips).catch(err => {
            console.error('Failed to load categories:', err);
            logError({ type: 'explicit', message: err.message, stack: err?.stack, url: location.href });
        });
    }

    // Load favourites when entering favourites page
    if (page === 'favourites') {
        renderFavouritesPage();
    }
}


// ── Settings page ────────────────────────────────────────────────────────────

function renderSettingsPanel() {
    const list = document.getElementById('settingsList');
    list.innerHTML = '';

    for (const ch of state.channels) {
        const hidden = isHidden(ch.id);
        const fav    = isFavourite(ch.id);

        const item = document.createElement('div');
        item.className = 'settings-item' + (hidden ? ' is-hidden' : '');

        // Favourite toggle
        const favBtn = document.createElement('button');
        favBtn.className = 'settings-icon-btn fav-btn' + (fav ? ' active' : '');
        favBtn.textContent = '\u2605'; // ★
        favBtn.title = fav ? 'Remove from favourites' : 'Add to favourites';
        favBtn.addEventListener('click', () => {
            toggleFavourite(ch.id);
            renderGuide();
            renderSettingsPanel();
        });

        // Channel name
        const name = document.createElement('span');
        name.className   = 'settings-name';
        name.textContent = ch.displayName;

        // Hide/show toggle
        const hideBtn = document.createElement('button');
        hideBtn.className = 'settings-icon-btn hide-btn' + (hidden ? ' active' : '');
        hideBtn.textContent = hidden ? '\u{1F648}' : '\u{1F441}'; // 🙈 / 👁
        hideBtn.title = hidden ? 'Show channel' : 'Hide channel';
        hideBtn.addEventListener('click', () => {
            toggleHidden(ch.id);
            renderGuide();
            renderSettingsPanel();
        });

        item.appendChild(favBtn);
        item.appendChild(name);
        item.appendChild(hideBtn);
        list.appendChild(item);
    }
}

// ── Search ───────────────────────────────────────────────────────────────────

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
        showError(`Search failed: ${err.message}`);
        logError({ type: 'explicit', message: err.message, stack: err?.stack, url: location.href });
        document.getElementById('searchResults').innerHTML =
            `<div class="search-empty">Search failed: ${err.message}</div>`;
    } finally {
        document.getElementById('searchSpinner').classList.remove('visible');
    }
}

function triggerSearch() {
    clearTimeout(state.searchDebounce);
    state.searchDebounce = setTimeout(performSearch, 300);
}

function renderSearchResults() {
    const container = document.getElementById('searchResults');
    container.innerHTML = '';

    // Build a channel name lookup from state.channels
    const channelMap = {};
    for (const ch of state.channels) {
        channelMap[ch.id] = ch;
    }

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

function renderCategoryChips() {
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

function setupSearchPage() {
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

// ── Favourites page ─────────────────────────────────────────────────────────

function renderFavouritesPage() {
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
            showError(`Favourite search "${fav.name}" failed: ${err.message}`);
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

    const channelMap = {};
    for (const ch of state.channels) {
        channelMap[ch.id] = ch;
    }

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

// ── Initialisation ────────────────────────────────────────────────────────────

async function init() {
    // Populate localStorage-dependent state fields
    state.prefs = loadPrefs();
    state.favouriteSearches = loadFavouriteSearches();

    // Resolve the active page from the URL path
    const initialPage = getPageFromPath();

    // Resolve the active date from the URL (falls back to today)
    state.currentDate = getDateFromURL();

    // Render static parts that don't depend on data
    renderTimeAxis();
    setupScrollSync();
    setupSearchPage();

    document.getElementById('dateDisplay').textContent = formatDateLong(state.currentDate);

    // Wire up controls
    document.getElementById('nowBtn').addEventListener('click', () => {
        if (state.currentDate === getTodayString()) {
            scrollToNow();
        } else {
            navigateToDate(getTodayString());
        }
    });
    document.getElementById('prevDay').addEventListener('click', () => navigateToDate(addDays(state.currentDate, -1)));
    document.getElementById('nextDay').addEventListener('click', () => navigateToDate(addDays(state.currentDate, 1)));
    document.getElementById('modalClose').addEventListener('click', closeModal);
    document.getElementById('modalBackdrop').addEventListener('click', (e) => {
        if (e.target === e.currentTarget) closeModal();
    });

    // Bottom nav tab switching
    document.querySelectorAll('.bottom-nav-btn').forEach(btn => {
        btn.addEventListener('click', () => navigateToPage(btn.dataset.page));
    });

    // Handle browser back/forward
    window.addEventListener('popstate', () => {
        const page = getPageFromPath();
        navigateToPage(page, { pushState: false });
        if (page === 'guide') {
            navigateToDate(getDateFromURL(), { pushState: false });
        }
    });

    // Show the initial page (without pushing state — we're already on this URL)
    navigateToPage(initialPage, { pushState: false });

    // Load data
    try {
        const [channels, programmes] = await Promise.all([
            fetchChannels(),
            fetchGuide(state.currentDate),
        ]);

        state.channels   = channels;
        state.programmes = programmes;

        const hasData = hasAiringsStartingOn(state.programmes, state.currentDate);
        document.getElementById('guideEmpty').classList.toggle('visible', !hasData);
        document.querySelector('.guide-container').classList.toggle('no-data', !hasData);
        renderGuide();
        renderSettingsPanel();

        // Small delay lets the browser complete layout before scrolling
        setTimeout(() => {
            if (state.currentDate === getTodayString() && state.activePage === 'guide') scrollToNow();
        }, 50);

    } catch (err) {
        showError(`Failed to load guide data: ${err.message}`);
        logError({ type: 'explicit', message: err.message, stack: err?.stack, url: location.href });
        document.getElementById('loadingText').textContent =
            `Failed to load guide data. Is the server running?\n${err.message}`;
        return; // leave loading screen visible as an error state
    }

    document.getElementById('loadingScreen').classList.add('hidden');
}

// ── Service worker registration ───────────────────────────────────────────────

if ('serviceWorker' in navigator) {
    // Reload the page when a new service worker takes control, so users
    // always see the latest assets without needing to force-quit the PWA.
    let refreshing = false;
    navigator.serviceWorker.addEventListener('controllerchange', () => {
        if (!refreshing) {
            refreshing = true;
            window.location.reload();
        }
    });

    navigator.serviceWorker.register('/sw.js').then(reg => {
        // Periodically check for SW updates (every 60s) so long-lived
        // PWA sessions pick up new deployments promptly.
        setInterval(() => reg.update(), 60 * 1000);
    }).catch(err => {
        console.warn('Service worker registration failed:', err);
    });
}

document.addEventListener('DOMContentLoaded', init);
