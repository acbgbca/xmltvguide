'use strict';

// ── Configuration ────────────────────────────────────────────────────────────
//
// PX_PER_MIN controls the horizontal zoom level of the guide.
// ROW_HEIGHT must match the --row-height CSS custom property in style.css.
// Changing either value here is the only thing needed to adjust the layout.

const CONFIG = {
    PX_PER_MIN:     4,    // pixels per minute → 4 = 240px/hr, 2 hours = 480px on screen
    ROW_HEIGHT:     54,   // px — must match --row-height in style.css
    LABEL_INTERVAL: 30,   // minutes between time-axis labels
    MINS_IN_DAY:    1440,
    get TOTAL_WIDTH() { return this.MINS_IN_DAY * this.PX_PER_MIN; },
};

// ── Application state ─────────────────────────────────────────────────────────

const state = {
    channels:    [],
    programmes:  [],
    currentDate: getTodayString(),  // 'YYYY-MM-DD' in browser local time
    prefs:       loadPrefs(),
    activePage:  'guide',           // current page: guide | search | favourites | settings
    nowLineTimer: null,             // interval ID for the now-line updater
    categories:    [],              // cached from /api/categories
    searchResults: [],              // current search results
    searchDebounce: null,           // debounce timer ID
    selectedCategories: new Set(),  // currently selected category filters
};

// ── Preferences (localStorage) ───────────────────────────────────────────────

function loadPrefs() {
    try {
        const raw = localStorage.getItem('tvguide-prefs');
        const p = raw ? JSON.parse(raw) : {};
        return { hidden: p.hidden || {}, favourites: p.favourites || {} };
    } catch {
        return { hidden: {}, favourites: {} };
    }
}

function savePrefs() {
    localStorage.setItem('tvguide-prefs', JSON.stringify(state.prefs));
}

function isHidden(channelId)    { return !!state.prefs.hidden[channelId]; }
function isFavourite(channelId) { return !!state.prefs.favourites[channelId]; }

function toggleHidden(channelId) {
    state.prefs.hidden[channelId] = !state.prefs.hidden[channelId];
    savePrefs();
    renderGuide();
}

function toggleFavourite(channelId) {
    state.prefs.favourites[channelId] = !state.prefs.favourites[channelId];
    savePrefs();
    renderGuide();
    renderSettingsPanel();
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
        });
    }
}

// ── URL state management ──────────────────────────────────────────────────────

function getDateFromURL() {
    const params = new URLSearchParams(window.location.search);
    const date = params.get('date');
    if (date && /^\d{4}-\d{2}-\d{2}$/.test(date)) return date;
    return getTodayString();
}

function setDateInURL(dateStr) {
    const url = new URL(window.location.href);
    // Ensure we stay on the guide path
    if (url.pathname !== '/' && url.pathname !== '/guide') {
        url.pathname = '/';
    }
    if (dateStr === getTodayString()) {
        url.searchParams.delete('date');
    } else {
        url.searchParams.set('date', dateStr);
    }
    history.pushState(null, '', url);
}

// ── Date / time utilities ─────────────────────────────────────────────────────

function getTodayString() {
    const now = new Date();
    const y = now.getFullYear();
    const m = String(now.getMonth() + 1).padStart(2, '0');
    const d = String(now.getDate()).padStart(2, '0');
    return `${y}-${m}-${d}`;
}

// Returns a Date representing midnight local time for a 'YYYY-MM-DD' string.
function dateMidnight(dateStr) {
    const [y, m, d] = dateStr.split('-').map(Number);
    return new Date(y, m - 1, d, 0, 0, 0, 0);
}

function formatTime(date) {
    return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', hour12: false });
}

function formatDateLong(dateStr) {
    const [y, m, d] = dateStr.split('-').map(Number);
    return new Date(y, m - 1, d).toLocaleDateString([], {
        weekday: 'short', day: 'numeric', month: 'short',
    });
}

// Formats a minute-offset-from-midnight as "HH:MM".
function minutesToHHMM(minutes) {
    const h = Math.floor(minutes / 60) % 24;
    const m = minutes % 60;
    return `${String(h).padStart(2, '0')}:${String(m).padStart(2, '0')}`;
}

// Returns a new date string offset by `days` from the given 'YYYY-MM-DD' string.
function addDays(dateStr, days) {
    const [y, m, d] = dateStr.split('-').map(Number);
    const date = new Date(y, m - 1, d + days);
    return [
        date.getFullYear(),
        String(date.getMonth() + 1).padStart(2, '0'),
        String(date.getDate()).padStart(2, '0'),
    ].join('-');
}

// Loads the guide for `dateStr`, updates state, re-renders, and scrolls.
// Pass { pushState: false } when called from a popstate handler.
async function navigateToDate(dateStr, { pushState = true } = {}) {
    document.getElementById('guideLoadingOverlay').classList.add('visible');
    document.getElementById('guideEmpty').classList.remove('visible');
    document.getElementById('prevDay').disabled = true;
    document.getElementById('nextDay').disabled = true;

    state.currentDate = dateStr;
    if (pushState) setDateInURL(dateStr);
    document.getElementById('dateDisplay').textContent = formatDateLong(dateStr);

    try {
        state.programmes = await fetchGuide(dateStr);
        if (!hasAiringsStartingOn(state.programmes, dateStr)) {
            document.getElementById('guideEmpty').classList.add('visible');
        }
        renderGuide();
        if (dateStr === getTodayString()) {
            scrollToNow();
        }
        // For other dates, preserve the current horizontal scroll position.
        await updateNavButtons();
    } catch (err) {
        console.error('Failed to navigate to', dateStr, err);
        document.getElementById('prevDay').disabled = true;
        document.getElementById('nextDay').disabled = true;
    } finally {
        document.getElementById('guideLoadingOverlay').classList.remove('visible');
    }
}

// Returns true only if any airing in `airings` starts within `dateStr`'s
// calendar day (local time). Airings that merely overlap from the previous
// day (stop_time crosses midnight) are excluded, so a day that contains
// nothing but spillover is treated as having no data.
function hasAiringsStartingOn(airings, dateStr) {
    const dayStart  = dateMidnight(dateStr);
    const dayEnd    = dateMidnight(addDays(dateStr, 1));
    return airings.some(p => {
        const start = new Date(p.start);
        return start >= dayStart && start < dayEnd;
    });
}

// Probes the guide API for the previous and next days and enables/disables
// the navigation buttons based on whether data exists for those dates.
// Uses Promise.allSettled so a failure on one side never affects the other
// button, and the function itself never throws.
async function updateNavButtons() {
    const prevDate = addDays(state.currentDate, -1);
    const nextDate = addDays(state.currentDate,  1);
    const [prevResult, nextResult] = await Promise.allSettled([
        fetchGuide(prevDate),
        fetchGuide(nextDate),
    ]);
    if (prevResult.status === 'fulfilled') {
        document.getElementById('prevDay').disabled = !hasAiringsStartingOn(prevResult.value, prevDate);
    }
    if (nextResult.status === 'fulfilled') {
        document.getElementById('nextDay').disabled = !hasAiringsStartingOn(nextResult.value, nextDate);
    }
}

// ── API ───────────────────────────────────────────────────────────────────────

async function fetchChannels() {
    const res = await fetch('/api/channels');
    if (!res.ok) throw new Error(`/api/channels returned ${res.status}`);
    return res.json();
}

async function fetchGuide(dateStr) {
    const res = await fetch(`/api/guide?date=${dateStr}`);
    if (!res.ok) throw new Error(`/api/guide returned ${res.status}`);
    return res.json();
}

// ── Channel ordering ──────────────────────────────────────────────────────────

// Returns visible channels with favourites first, then remaining channels
// sorted by lcn (if present), falling back to source order for channels
// without an lcn.
function orderedVisibleChannels() {
    const visible = state.channels.filter(ch => !isHidden(ch.id));
    const favs    = visible.filter(ch =>  isFavourite(ch.id));
    const rest    = visible.filter(ch => !isFavourite(ch.id));
    rest.sort((a, b) => {
        if (a.lcn != null && b.lcn != null) return a.lcn - b.lcn;
        if (a.lcn != null) return -1;
        if (b.lcn != null) return  1;
        return 0; // both absent — preserve source order
    });
    return [...favs, ...rest];
}

// ── Guide rendering ───────────────────────────────────────────────────────────

function renderGuide() {
    const midnight = dateMidnight(state.currentDate);
    const channels = orderedVisibleChannels();

    // Group programmes by channelId for fast lookup.
    const byChannel = {};
    for (const p of state.programmes) {
        (byChannel[p.channelId] ??= []).push(p);
    }

    const totalHeight = channels.length * CONFIG.ROW_HEIGHT;

    // ── Channel column ──
    const channelCol = document.getElementById('channelCol');
    channelCol.innerHTML = '';
    channelCol.style.height = totalHeight + 'px';

    // ── Programmes inner container ──
    const inner = document.getElementById('programmesInner');
    // Remove all children except the now-line
    const nowLine = document.getElementById('nowLine');
    inner.innerHTML = '';
    inner.appendChild(nowLine);
    inner.style.width  = CONFIG.TOTAL_WIDTH + 'px';
    inner.style.height = totalHeight + 'px';

    for (let i = 0; i < channels.length; i++) {
        const ch  = channels[i];
        const top = i * CONFIG.ROW_HEIGHT;

        // Channel label
        const label = document.createElement('div');
        label.className = 'channel-label' + (isFavourite(ch.id) ? ' is-favourite' : '');
        label.style.top    = top + 'px';
        label.style.height = CONFIG.ROW_HEIGHT + 'px';

        // Top row: icon + LCN
        const topRow = document.createElement('div');
        topRow.className = 'channel-top-row';
        if (ch.icon) {
            const img = document.createElement('img');
            img.src       = ch.icon;
            img.alt       = '';
            img.className = 'channel-icon';
            img.onerror   = () => img.remove();
            topRow.appendChild(img);
        }
        if (ch.lcn != null) {
            const lcnEl = document.createElement('span');
            lcnEl.className   = 'channel-lcn';
            lcnEl.textContent = ch.lcn;
            topRow.appendChild(lcnEl);
        }
        label.appendChild(topRow);

        // Bottom row: channel name
        const nameEl = document.createElement('span');
        nameEl.className   = 'channel-name';
        nameEl.textContent = ch.displayName;
        label.appendChild(nameEl);
        channelCol.appendChild(label);

        // Programme row
        const row = document.createElement('div');
        row.className  = 'programme-row';
        row.style.top  = top + 'px';
        row.style.height = CONFIG.ROW_HEIGHT + 'px';
        row.style.width  = CONFIG.TOTAL_WIDTH + 'px';

        for (const prog of (byChannel[ch.id] ?? [])) {
            const cell = buildProgrammeCell(prog, midnight);
            if (cell) row.appendChild(cell);
        }

        inner.appendChild(row);
    }

    updateNowLine(midnight);
}

function buildProgrammeCell(prog, midnight) {
    const start = new Date(prog.start);
    const stop  = new Date(prog.stop);

    const startMin = (start - midnight) / 60_000;
    const stopMin  = (stop  - midnight) / 60_000;

    // Skip if entirely outside today
    if (stopMin <= 0 || startMin >= CONFIG.MINS_IN_DAY) return null;

    const clampStart = Math.max(0, startMin);
    const clampStop  = Math.min(CONFIG.MINS_IN_DAY, stopMin);
    const durMin     = clampStop - clampStart;
    if (durMin <= 0) return null;

    const left  = clampStart * CONFIG.PX_PER_MIN;
    const width = durMin * CONFIG.PX_PER_MIN - 2; // 2px gap between cells

    const el = document.createElement('div');
    el.className = 'programme' + (width < 60 ? ' narrow' : '');
    el.style.left  = left + 'px';
    el.style.width = width + 'px';

    const titleEl = document.createElement('span');
    titleEl.className   = 'prog-title';
    titleEl.textContent = prog.title;
    el.appendChild(titleEl);

    const timeEl = document.createElement('span');
    timeEl.className   = 'prog-time';
    timeEl.textContent = formatTime(start);
    el.appendChild(timeEl);

    el.title = `${prog.title}\n${formatTime(start)} – ${formatTime(stop)}`;

    el.addEventListener('click', () => openProgrammeModal(prog));

    return el;
}

// ── Now line ──────────────────────────────────────────────────────────────────

function updateNowLine(midnight) {
    const nowLine = document.getElementById('nowLine');
    const minFromMidnight = (Date.now() - midnight) / 60_000;

    const isToday = state.currentDate === getTodayString();
    if (!isToday || minFromMidnight < 0 || minFromMidnight > CONFIG.MINS_IN_DAY) {
        nowLine.style.display = 'none';
        return;
    }

    nowLine.style.display = 'block';
    nowLine.style.left    = (minFromMidnight * CONFIG.PX_PER_MIN) + 'px';
    nowLine.style.height  = document.getElementById('programmesInner').style.height;
}

function startNowLineTimer() {
    if (state.nowLineTimer) return;
    state.nowLineTimer = setInterval(() => updateNowLine(dateMidnight(state.currentDate)), 60_000);
}

function stopNowLineTimer() {
    if (state.nowLineTimer) {
        clearInterval(state.nowLineTimer);
        state.nowLineTimer = null;
    }
}

// ── Time axis ─────────────────────────────────────────────────────────────────

function renderTimeAxis() {
    const axis = document.getElementById('timeAxis');
    axis.innerHTML = '';
    axis.style.width = CONFIG.TOTAL_WIDTH + 'px';

    for (let m = 0; m < CONFIG.MINS_IN_DAY; m += CONFIG.LABEL_INTERVAL) {
        const label = document.createElement('div');
        label.className   = 'time-label';
        label.style.left  = (m * CONFIG.PX_PER_MIN) + 'px';
        label.textContent = minutesToHHMM(m);
        axis.appendChild(label);
    }
}

// ── Scroll sync ───────────────────────────────────────────────────────────────

function setupScrollSync() {
    const progOuter      = document.getElementById('programmesOuter');
    const timeAxisOuter  = document.getElementById('timeAxisOuter');
    const channelColOuter = document.getElementById('channelColOuter');

    progOuter.addEventListener('scroll', () => {
        timeAxisOuter.scrollLeft  = progOuter.scrollLeft;
        channelColOuter.scrollTop = progOuter.scrollTop;
    }, { passive: true });
}

function scrollToNow() {
    if (state.currentDate !== getTodayString()) return;

    const midnight        = dateMidnight(state.currentDate);
    const minFromMidnight = (Date.now() - midnight) / 60_000;
    const progOuter       = document.getElementById('programmesOuter');
    const centreOffset    = progOuter.clientWidth / 2;

    // Disable smooth scroll just for this jump so it snaps immediately on load
    progOuter.style.scrollBehavior = 'auto';
    progOuter.scrollLeft = Math.max(0, minFromMidnight * CONFIG.PX_PER_MIN - centreOffset);
    progOuter.style.scrollBehavior = '';
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
        favBtn.addEventListener('click', () => toggleFavourite(ch.id));

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
            renderSettingsPanel();
        });

        item.appendChild(favBtn);
        item.appendChild(name);
        item.appendChild(hideBtn);
        list.appendChild(item);
    }
}

// ── Programme details modal ───────────────────────────────────────────────────

function openProgrammeModal(prog) {
    const start = new Date(prog.start);
    const stop  = new Date(prog.stop);

    // Meta row: category · rating · year
    document.getElementById('modalCategory').textContent = prog.categories?.join(', ') ?? '';
    document.getElementById('modalRating').textContent   = prog.contentRating ?? '';
    document.getElementById('modalYear').textContent     = prog.year ?? '';
    setVisible('modalRating', !!prog.contentRating);
    setVisible('modalYear',   !!prog.year);

    document.getElementById('modalTitle').textContent = prog.title;
    document.getElementById('modalTime').textContent  = `${formatTime(start)} – ${formatTime(stop)}`;

    // Episode number (prefer human-readable display format, fall back to xmltv_ns)
    const epNum = prog.episodeNumDisplay || prog.episodeNum || '';
    document.getElementById('modalEpisode').textContent = epNum;
    setVisible('modalEpisode', !!epNum);

    document.getElementById('modalSubtitle').textContent = prog.subTitle ?? '';
    setVisible('modalSubtitle', !!prog.subTitle);

    // Badges: Repeat / Premiere / Star rating
    const badges = document.getElementById('modalBadges');
    badges.innerHTML = '';
    if (prog.isRepeat)   addBadge(badges, 'Repeat');
    if (prog.isPremiere) addBadge(badges, 'Premiere');
    if (prog.starRating) addBadge(badges, prog.starRating);

    document.getElementById('modalDesc').textContent = prog.description || 'No description available.';

    document.getElementById('modalBackdrop').classList.add('open');
}

function setVisible(id, visible) {
    document.getElementById(id).style.display = visible ? '' : 'none';
}

function addBadge(container, text) {
    const b = document.createElement('span');
    b.className   = 'badge';
    b.textContent = text;
    container.appendChild(b);
}

function closeModal() {
    document.getElementById('modalBackdrop').classList.remove('open');
}

// ── Search ───────────────────────────────────────────────────────────────────

async function fetchCategories() {
    if (state.categories.length > 0) return state.categories;
    const res = await fetch('/api/categories');
    if (!res.ok) throw new Error(`/api/categories returned ${res.status}`);
    state.categories = await res.json();
    return state.categories;
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
        console.error('Search failed:', err);
        document.getElementById('searchResults').innerHTML =
            '<div class="search-empty">Search failed. Please try again.</div>';
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
        starBtn.textContent = '\u2606'; // ☆
        starBtn.title = 'Save as favourite';
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

function openSearchAiringModal(airing, title) {
    // Adapt the search airing shape to the programme shape used by openProgrammeModal
    const prog = {
        title:             title,
        start:             airing.startTime,
        stop:              airing.stopTime,
        subTitle:          airing.subTitle,
        description:       airing.description,
        categories:        airing.categories,
        episodeNumDisplay: airing.episodeNumDisplay,
        isRepeat:          airing.isRepeat,
        isPremiere:        airing.isPremiere,
    };
    openProgrammeModal(prog);
}

function formatSearchDate(date) {
    const now = new Date();
    const today = new Date(now.getFullYear(), now.getMonth(), now.getDate());
    const tomorrow = new Date(today);
    tomorrow.setDate(tomorrow.getDate() + 1);
    const weekAhead = new Date(today);
    weekAhead.setDate(weekAhead.getDate() + 7);

    const dateOnly = new Date(date.getFullYear(), date.getMonth(), date.getDate());
    const time = date.toLocaleTimeString([], { hour: 'numeric', minute: '2-digit', hour12: true });

    if (dateOnly.getTime() === today.getTime()) {
        return 'Today ' + time;
    }
    if (dateOnly.getTime() === tomorrow.getTime()) {
        return 'Tomorrow ' + time;
    }
    if (dateOnly < weekAhead) {
        return date.toLocaleDateString([], { weekday: 'short' }) + ' ' + time;
    }
    return date.toLocaleDateString([], { weekday: 'short', day: 'numeric', month: 'short' }) + ' ' + time;
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

// ── Initialisation ────────────────────────────────────────────────────────────

async function init() {
    // Resolve the active page from the URL path
    const initialPage = getPageFromPath();

    // Resolve the active date from the URL (falls back to today)
    state.currentDate = getDateFromURL();

    // Render static parts that don't depend on data
    renderTimeAxis();
    setupScrollSync();
    setupSearchPage();

    document.getElementById('dateDisplay').textContent = formatDateLong(state.currentDate);

    // Disable nav buttons until we know which adjacent days have data
    document.getElementById('prevDay').disabled = true;
    document.getElementById('nextDay').disabled = true;

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

        if (!hasAiringsStartingOn(state.programmes, state.currentDate)) {
            document.getElementById('guideEmpty').classList.add('visible');
        }
        renderGuide();
        renderSettingsPanel();

        // Small delay lets the browser complete layout before scrolling
        setTimeout(() => {
            if (state.currentDate === getTodayString() && state.activePage === 'guide') scrollToNow();
        }, 50);

    } catch (err) {
        console.error('Failed to load guide data:', err);
        document.getElementById('loadingText').textContent =
            'Failed to load guide data. Is the server running?';
        return; // leave loading screen visible as an error state
    }

    // Run outside the server-error catch so a probe failure here never
    // produces the misleading "Is the server running?" message.
    await updateNavButtons();

    document.getElementById('loadingScreen').classList.add('hidden');
}

// ── Service worker registration ───────────────────────────────────────────────

if ('serviceWorker' in navigator) {
    navigator.serviceWorker.register('/sw.js').catch(err => {
        console.warn('Service worker registration failed:', err);
    });
}

document.addEventListener('DOMContentLoaded', init);
