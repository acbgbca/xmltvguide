import { formatTime } from '../utils/date.js';

const MODES = [
    { id: 'now-next',   label: 'Now/Next' },
    { id: 'categories', label: 'Categories' },
    { id: 'premieres',  label: 'Premieres' },
    { id: 'time-slot',  label: 'Time Slot' },
];

function getModeFromURL() {
    const params = new URLSearchParams(window.location.search);
    const mode = params.get('mode');
    return MODES.some(m => m.id === mode) ? mode : 'now-next';
}

export function loadExplorePage() {
    const container = document.getElementById('page-explore');
    container.innerHTML = '';

    const activeMode = getModeFromURL();

    // Mode switcher
    const switcher = document.createElement('div');
    switcher.className = 'explore-mode-switcher';

    for (const mode of MODES) {
        const btn = document.createElement('button');
        btn.className = 'explore-mode-btn' + (mode.id === activeMode ? ' active' : '');
        btn.dataset.mode = mode.id;
        btn.textContent = mode.label;
        btn.addEventListener('click', () => {
            if (getModeFromURL() === mode.id) return;
            history.pushState({}, '', '/explore?mode=' + mode.id);
            loadExplorePage();
        });
        switcher.appendChild(btn);
    }

    container.appendChild(switcher);

    // Content area
    const content = document.createElement('div');
    content.className = 'explore-content';

    if (activeMode === 'now-next') {
        renderNowNextMode(content);
    } else {
        content.innerHTML = '<p>Coming soon</p>';
    }

    container.appendChild(content);
}

async function renderNowNextMode(container) {
    const loading = document.createElement('div');
    loading.className = 'explore-loading';
    loading.textContent = 'Loading…';
    container.appendChild(loading);

    let entries;
    try {
        const res = await fetch('/api/explore/now-next');
        if (!res.ok) throw new Error(`/api/explore/now-next returned ${res.status}`);
        entries = await res.json();
    } catch (err) {
        container.innerHTML = '';
        const error = document.createElement('div');
        error.className = 'explore-error';
        error.textContent = 'Failed to load data. Please try again.';
        container.appendChild(error);
        return;
    }

    container.innerHTML = '';

    const now = Date.now();

    // Sort: channels with both null go to the bottom
    const withData = entries.filter(e => e.current !== null || e.next !== null);
    const nullBoth = entries.filter(e => e.current === null && e.next === null);
    const sorted = [...withData, ...nullBoth];

    const list = document.createElement('div');
    list.className = 'now-next-list';

    for (const entry of sorted) {
        const row = document.createElement('div');
        row.className = 'now-next-row';
        row.dataset.channelId = entry.channelId;

        // Channel name
        const channelName = document.createElement('div');
        channelName.className = 'now-next-channel';
        channelName.textContent = entry.channelName;
        row.appendChild(channelName);

        // Current show
        const currentDiv = document.createElement('div');
        currentDiv.className = 'now-next-current';
        if (entry.current) {
            const stopTime = new Date(entry.current.stop).getTime();
            const minsRemaining = Math.floor((stopTime - now) / 60000);

            const badge = document.createElement('span');
            badge.className = 'now-badge';
            badge.textContent = 'now';
            currentDiv.appendChild(badge);

            const title = document.createElement('span');
            title.className = 'now-next-title';
            title.textContent = entry.current.title;
            currentDiv.appendChild(title);

            const remaining = document.createElement('span');
            remaining.className = 'now-next-remaining';
            remaining.textContent = `ends in ${minsRemaining} min`;
            currentDiv.appendChild(remaining);
        } else {
            currentDiv.textContent = 'Nothing airing';
        }
        row.appendChild(currentDiv);

        // Next show
        if (entry.next) {
            const nextDiv = document.createElement('div');
            nextDiv.className = 'now-next-next';

            const nextTitle = document.createElement('span');
            nextTitle.className = 'now-next-title';
            nextTitle.textContent = entry.next.title;
            nextDiv.appendChild(nextTitle);

            const nextTime = document.createElement('span');
            nextTime.className = 'now-next-time';
            nextTime.textContent = formatTime(new Date(entry.next.start));
            nextDiv.appendChild(nextTime);

            row.appendChild(nextDiv);
        }

        list.appendChild(row);
    }

    container.appendChild(list);
}
