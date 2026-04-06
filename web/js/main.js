import { getTodayString, addDays, formatDateLong } from './utils/date.js';
import { state } from './state.js';
import { fetchChannels, fetchGuide, logError } from './api.js';
import { loadPrefs } from './store/preferences.js';
import { loadFavouriteSearches } from './store/favourites.js';
import { closeModal } from './components/modal.js';
import {
    renderGuide, renderTimeAxis, setupScrollSync, scrollToNow,
    navigateToDate, hasAiringsStartingOn,
    getDateFromURL,
} from './pages/guide.js';
import { setupSearchPage } from './pages/search.js';
import { renderSettingsPanel } from './pages/settings.js';
import { getPageFromPath, navigateToPage } from './router.js';

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
        console.error(`Failed to load guide data: ${err.message}`);
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
