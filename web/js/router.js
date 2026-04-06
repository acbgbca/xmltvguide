import { state } from './state.js';
import { startNowLineTimer, stopNowLineTimer } from './pages/guide.js';
import { loadSearchPageCategories } from './pages/search.js';
import { renderFavouritesPage } from './pages/favourites.js';
import { renderSettingsPanel } from './pages/settings.js';

// ── Routing ──────────────────────────────────────────────────────────────────

export const PAGES = ['guide', 'search', 'favourites', 'settings'];

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
