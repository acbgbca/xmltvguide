import { getTodayString, addDays, formatDateLong } from './utils/date.js';
import { state } from './state.js';
import { fetchChannels, fetchGuide, fetchCategories, logError } from './api.js';
import { loadPrefs, isHidden, isFavourite, toggleHidden, toggleFavourite } from './store/preferences.js';
import { loadFavouriteSearches } from './store/favourites.js';
import { closeModal } from './components/modal.js';
import {
    renderGuide, renderTimeAxis, setupScrollSync, scrollToNow,
    startNowLineTimer, stopNowLineTimer,
    navigateToDate, hasAiringsStartingOn,
    getDateFromURL,
} from './pages/guide.js';
import {
    setupSearchPage, triggerSearch, renderCategoryChips, getCurrentSearchConfig,
    loadSearchPageCategories,
} from './pages/search.js';
import { initFavouritesPage, renderFavouritesPage } from './pages/favourites.js';

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
        loadSearchPageCategories();
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

// ── Initialisation ────────────────────────────────────────────────────────────

async function init() {
    // Populate localStorage-dependent state fields
    state.prefs = loadPrefs();
    state.favouriteSearches = loadFavouriteSearches();

    // Resolve the active page from the URL path
    const initialPage = getPageFromPath();

    // Resolve the active date from the URL (falls back to today)
    state.currentDate = getDateFromURL();

    // Wire up cross-page callbacks
    initFavouritesPage({ editFavouriteSearch });

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
