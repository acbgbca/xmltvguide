import { CONFIG } from '../config.js';
import { getTodayString, dateMidnight, formatTime, formatDateLong, minutesToHHMM, addDays } from '../utils/date.js';
import { state } from '../state.js';
import { fetchGuide } from '../api.js';
import { isHidden, isFavourite } from '../store/preferences.js';
import { openProgrammeModal } from '../components/modal.js';

// ── URL state management ──────────────────────────────────────────────────────

export function getDateFromURL() {
    const params = new URLSearchParams(window.location.search);
    const date = params.get('date');
    if (date && /^\d{4}-\d{2}-\d{2}$/.test(date)) return date;
    return getTodayString();
}

export function setDateInURL(dateStr) {
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

export function renderGuide() {
    const midnight = dateMidnight(state.currentDate);
    const channels = orderedVisibleChannels();

    // Group programmes by channelId for fast lookup.
    const byChannel = {};
    for (const p of state.programmes) {
        (byChannel[p.channelId] ??= []).push(p);
    }

    const bottomNavHeight = parseInt(getComputedStyle(document.documentElement).getPropertyValue('--bottom-nav-height')) || 56;
    const totalHeight = channels.length * CONFIG.ROW_HEIGHT + bottomNavHeight;

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

export function startNowLineTimer() {
    if (state.nowLineTimer) return;
    state.nowLineTimer = setInterval(() => updateNowLine(dateMidnight(state.currentDate)), 60_000);
}

export function stopNowLineTimer() {
    if (state.nowLineTimer) {
        clearInterval(state.nowLineTimer);
        state.nowLineTimer = null;
    }
}

// ── Time axis ─────────────────────────────────────────────────────────────────

export function renderTimeAxis() {
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

export function setupScrollSync() {
    const progOuter      = document.getElementById('programmesOuter');
    const timeAxisOuter  = document.getElementById('timeAxisOuter');
    const channelColOuter = document.getElementById('channelColOuter');

    progOuter.addEventListener('scroll', () => {
        timeAxisOuter.scrollLeft  = progOuter.scrollLeft;
        channelColOuter.scrollTop = progOuter.scrollTop;
    }, { passive: true });
}

export function scrollToNow() {
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

// ── Date navigation ───────────────────────────────────────────────────────────

// Returns true only if any airing in `airings` starts within `dateStr`'s
// calendar day (local time). Airings that merely overlap from the previous
// day (stop_time crosses midnight) are excluded, so a day that contains
// nothing but spillover is treated as having no data.
export function hasAiringsStartingOn(airings, dateStr) {
    const dayStart  = dateMidnight(dateStr);
    const dayEnd    = dateMidnight(addDays(dateStr, 1));
    return airings.some(p => {
        const start = new Date(p.start);
        return start >= dayStart && start < dayEnd;
    });
}

// Loads the guide for `dateStr`, updates state, re-renders, and scrolls.
// Pass { pushState: false } when called from a popstate handler.
export async function navigateToDate(dateStr, { pushState = true } = {}) {
    document.getElementById('guideLoadingOverlay').classList.add('visible');
    document.getElementById('guideEmpty').classList.remove('visible');
    document.querySelector('.guide-container').classList.remove('no-data');

    state.currentDate = dateStr;
    if (pushState) setDateInURL(dateStr);
    document.getElementById('dateDisplay').textContent = formatDateLong(dateStr);

    try {
        state.programmes = await fetchGuide(dateStr);
        const hasData = hasAiringsStartingOn(state.programmes, dateStr);
        document.getElementById('guideEmpty').classList.toggle('visible', !hasData);
        document.querySelector('.guide-container').classList.toggle('no-data', !hasData);
        renderGuide();
        if (hasData && dateStr === getTodayString()) {
            scrollToNow();
        }
        // For other dates, preserve the current horizontal scroll position.
    } catch (err) {
        console.error('Failed to navigate to', dateStr, err);
    } finally {
        document.getElementById('guideLoadingOverlay').classList.remove('visible');
    }
}
