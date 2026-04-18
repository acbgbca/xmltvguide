import { state } from './state.js';
import { startNowLineTimer, stopNowLineTimer } from './pages/guide.js';
import { loadSearchPageCategories } from './pages/search.js';
import { renderFavouritesPage } from './pages/favourites.js';
import { renderSettingsPanel } from './pages/settings.js';
import { loadExplorePage } from './pages/explore.js';
import { getTodayString } from './utils/date.js';

// ── Routing ──────────────────────────────────────────────────────────────────

export const PAGES = ['guide', 'search', 'favourites', 'explore', 'settings'];

const PAGE_INIT = {
    settings: renderSettingsPanel,
    search: loadSearchPageCategories,
    favourites: renderFavouritesPage,
    explore: loadExplorePage,
};

export function getPageFromPath() {
    const path = window.location.pathname.replace(/^\/+/, '').split('?')[0];
    if (PAGES.includes(path)) return path;
    return 'guide';
}

function buildURL(page) {
    if (page === 'guide') {
        const today = getTodayString();
        const dateParam = state.currentDate && state.currentDate !== today
            ? '?date=' + state.currentDate
            : '';
        return '/' + dateParam;
    }
    return '/' + page;
}

export function navigateToPage(page, { pushState = true } = {}) {
    if (!PAGES.includes(page)) page = 'guide';
    state.activePage = page;

    if (pushState) {
        history.pushState({}, '', buildURL(page));
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

    // Run any per-page initialisation
    PAGE_INIT[page]?.();
}
