import { state } from './state.js';
import { startNowLineTimer, stopNowLineTimer } from './pages/guide.js';
import { loadSearchPageCategories } from './pages/search.js';
import { renderFavouritesPage } from './pages/favourites.js';
import { renderSettingsPanel } from './pages/settings.js';
import { loadExplorePage } from './pages/explore.js';
import { getTodayString } from './utils/date.js';

// ── Routing ──────────────────────────────────────────────────────────────────

export const PAGES = ['guide', 'search', 'favourites', 'explore', 'settings'];

export function getPageFromPath() {
    const path = window.location.pathname.replace(/^\/+/, '').split('?')[0];
    if (PAGES.includes(path)) return path;
    return 'guide';
}

export function navigateToPage(page, { pushState = true } = {}) {
    if (!PAGES.includes(page)) page = 'guide';
    state.activePage = page;

    // Update URL
    if (pushState) {
        let url;
        if (page === 'guide') {
            // Restore the guide's current date parameter from state rather than
            // relying on window.location.search, which may be empty when returning
            // from another tab (e.g. /search has no query string).
            const today = getTodayString();
            const dateParam = state.currentDate && state.currentDate !== today
                ? '?date=' + state.currentDate
                : '';
            url = '/' + dateParam;
        } else {
            url = '/' + page;
        }
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

    // Load explore page when entering explore tab
    if (page === 'explore') {
        loadExplorePage();
    }
}
