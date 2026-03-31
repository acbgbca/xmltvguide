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

// ── URL state management ──────────────────────────────────────────────────────

function getDateFromURL() {
    const params = new URLSearchParams(window.location.search);
    const date = params.get('date');
    if (date && /^\d{4}-\d{2}-\d{2}$/.test(date)) return date;
    return getTodayString();
}

function setDateInURL(dateStr) {
    const url = new URL(window.location.href);
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
    document.getElementById('prevDay').disabled = true;
    document.getElementById('nextDay').disabled = true;

    state.currentDate = dateStr;
    if (pushState) setDateInURL(dateStr);
    document.getElementById('dateDisplay').textContent = formatDateLong(dateStr);

    try {
        state.programmes = await fetchGuide(dateStr);
        renderGuide();
        if (dateStr === getTodayString()) {
            scrollToNow();
        }
        // For other dates, preserve the current horizontal scroll position.
    } catch (err) {
        console.error('Failed to load guide for', dateStr, err);
    }

    await updateNavButtons();
    document.getElementById('guideLoadingOverlay').classList.remove('visible');
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
async function updateNavButtons() {
    const prevDate = addDays(state.currentDate, -1);
    const nextDate = addDays(state.currentDate,  1);
    const [prevData, nextData] = await Promise.all([
        fetchGuide(prevDate),
        fetchGuide(nextDate),
    ]);
    document.getElementById('prevDay').disabled = !hasAiringsStartingOn(prevData, prevDate);
    document.getElementById('nextDay').disabled = !hasAiringsStartingOn(nextData, nextDate);
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

// ── Settings panel ────────────────────────────────────────────────────────────

function openSettings() {
    document.getElementById('settingsPanel').classList.add('open');
    document.getElementById('settingsOverlay').classList.add('open');
}

function closeSettings() {
    document.getElementById('settingsPanel').classList.remove('open');
    document.getElementById('settingsOverlay').classList.remove('open');
}

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

// ── Initialisation ────────────────────────────────────────────────────────────

async function init() {
    // Resolve the active date from the URL (falls back to today)
    state.currentDate = getDateFromURL();

    // Render static parts that don't depend on data
    renderTimeAxis();
    setupScrollSync();

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
    document.getElementById('settingsBtn').addEventListener('click', openSettings);
    document.getElementById('settingsClose').addEventListener('click', closeSettings);
    document.getElementById('settingsOverlay').addEventListener('click', closeSettings);
    document.getElementById('modalClose').addEventListener('click', closeModal);
    document.getElementById('modalBackdrop').addEventListener('click', (e) => {
        if (e.target === e.currentTarget) closeModal();
    });

    // Sync navigation when the user presses the browser back/forward buttons
    window.addEventListener('popstate', () => navigateToDate(getDateFromURL(), { pushState: false }));

    // Load data
    try {
        const [channels, programmes] = await Promise.all([
            fetchChannels(),
            fetchGuide(state.currentDate),
        ]);

        state.channels   = channels;
        state.programmes = programmes;

        renderGuide();
        renderSettingsPanel();

        // Small delay lets the browser complete layout before scrolling
        setTimeout(() => {
            if (state.currentDate === getTodayString()) scrollToNow();
        }, 50);

        // Keep the now-line position current
        setInterval(() => updateNowLine(dateMidnight(state.currentDate)), 60_000);

        await updateNavButtons();

    } catch (err) {
        console.error('Failed to load guide data:', err);
        document.getElementById('loadingText').textContent =
            'Failed to load guide data. Is the server running?';
        return; // leave loading screen visible as an error state
    }

    document.getElementById('loadingScreen').classList.add('hidden');
}

// ── Service worker registration ───────────────────────────────────────────────

if ('serviceWorker' in navigator) {
    navigator.serviceWorker.register('/sw.js').catch(err => {
        console.warn('Service worker registration failed:', err);
    });
}

document.addEventListener('DOMContentLoaded', init);
