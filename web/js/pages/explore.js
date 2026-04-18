import { formatTime, formatSearchDate, getTodayString } from '../utils/date.js';
import { fetchCategories, fetchChannels } from '../api.js';
import { openSearchAiringModal } from '../components/modal.js';
import { state } from '../state.js';

const CLASS_LOADING = 'explore-loading';
const CLASS_ERROR   = 'explore-error';

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
    } else if (activeMode === 'categories') {
        renderCategoriesMode(content);
    } else if (activeMode === 'premieres') {
        renderPremieresMode(content);
    } else if (activeMode === 'time-slot') {
        renderTimeSlotMode(content);
    } else {
        content.innerHTML = '<p>Coming soon</p>';
    }

    container.appendChild(content);
}

async function renderCategoriesMode(container) {
    const params = new URLSearchParams(window.location.search);
    const selectedCategory = params.get('category');

    await (selectedCategory ? renderCategoryResults(container, selectedCategory) : renderCategoryPicker(container));
}

async function renderCategoryPicker(container) {
    const loading = document.createElement('div');
    loading.className = CLASS_LOADING;
    loading.textContent = 'Loading…';
    container.appendChild(loading);

    let categories;
    try {
        categories = await fetchCategories();
    } catch {
        container.innerHTML = '';
        const error = document.createElement('div');
        error.className = CLASS_ERROR;
        error.textContent = 'Failed to load categories. Please try again.';
        container.appendChild(error);
        return;
    }

    container.innerHTML = '';

    const picker = document.createElement('div');
    picker.className = 'category-picker';

    for (const cat of categories) {
        const btn = document.createElement('button');
        btn.className = 'category-picker-btn';
        btn.dataset.category = cat;
        btn.textContent = cat;
        btn.addEventListener('click', () => {
            history.pushState({}, '', '/explore?mode=categories&category=' + encodeURIComponent(cat));
            container.innerHTML = '';
            renderCategoryResults(container, cat);
        });
        picker.appendChild(btn);
    }

    container.appendChild(picker);
}

async function renderCategoryResults(container, category) {
    container.innerHTML = '';

    // Back button
    const backBtn = document.createElement('button');
    backBtn.className = 'category-back-btn';
    backBtn.textContent = '← Back';
    backBtn.addEventListener('click', () => {
        history.back();
    });
    container.appendChild(backBtn);

    // Results title
    const title = document.createElement('div');
    title.className = 'category-results-title';
    title.textContent = category;
    container.appendChild(title);

    // Loading indicator
    const loading = document.createElement('div');
    loading.className = CLASS_LOADING;
    loading.textContent = 'Loading…';
    container.appendChild(loading);

    let results;
    try {
        const searchParams = new URLSearchParams({
            mode: 'advanced',
            categories: category,
            include_past: 'false',
        });
        const res = await fetch('/api/search?' + searchParams);
        if (!res.ok) throw new Error(`/api/search returned ${res.status}`);
        results = await res.json();
    } catch {
        loading.remove();
        const error = document.createElement('div');
        error.className = CLASS_ERROR;
        error.textContent = 'Failed to load results. Please try again.';
        container.appendChild(error);
        return;
    }

    loading.remove();

    const resultsContainer = document.createElement('div');
    resultsContainer.className = 'category-results';

    if (results.length === 0) {
        const empty = document.createElement('div');
        empty.className = 'category-empty';
        empty.textContent = 'No upcoming programmes found in this category.';
        resultsContainer.appendChild(empty);
        container.appendChild(resultsContainer);
        return;
    }

    for (const group of results) {
        const groupEl = document.createElement('div');
        groupEl.className = 'search-group';

        const titleEl = document.createElement('h3');
        titleEl.className = 'search-group-title';
        titleEl.textContent = group.title;
        groupEl.appendChild(titleEl);

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

        resultsContainer.appendChild(groupEl);
    }

    container.appendChild(resultsContainer);
}

async function renderPremieresMode(container) {
    const loading = document.createElement('div');
    loading.className = CLASS_LOADING;
    loading.textContent = 'Loading…';
    container.appendChild(loading);

    let results;
    try {
        const res = await fetch('/api/search?is_premiere=true&include_past=false');
        if (!res.ok) throw new Error(`/api/search returned ${res.status}`);
        results = await res.json();
    } catch {
        container.innerHTML = '';
        const error = document.createElement('div');
        error.className = CLASS_ERROR;
        error.textContent = 'Failed to load premieres. Please try again.';
        container.appendChild(error);
        return;
    }

    container.innerHTML = '';

    // Flatten grouped results into a single list sorted by start time
    const airings = [];
    for (const group of results) {
        for (const airing of group.airings) {
            airings.push({ ...airing, title: group.title });
        }
    }
    airings.sort((a, b) => new Date(a.startTime) - new Date(b.startTime));

    if (airings.length === 0) {
        const empty = document.createElement('div');
        empty.className = 'premieres-empty';
        empty.textContent = 'No upcoming premieres found';
        container.appendChild(empty);
        return;
    }

    const list = document.createElement('div');
    list.className = 'premieres-list';

    for (const airing of airings) {
        const item = document.createElement('div');
        item.className = 'premiere-item';
        item.addEventListener('click', () => openSearchAiringModal(airing, airing.title));

        const titleEl = document.createElement('div');
        titleEl.className = 'premiere-title';
        titleEl.textContent = airing.title;
        item.appendChild(titleEl);

        const metaEl = document.createElement('div');
        metaEl.className = 'premiere-meta';

        const channelEl = document.createElement('span');
        channelEl.className = 'premiere-channel';
        channelEl.textContent = airing.channelName || airing.channelId;
        metaEl.appendChild(channelEl);

        const timeEl = document.createElement('span');
        timeEl.className = 'premiere-time';
        timeEl.textContent = formatSearchDate(new Date(airing.startTime));
        metaEl.appendChild(timeEl);

        item.appendChild(metaEl);

        if (airing.subTitle) {
            const subTitleEl = document.createElement('div');
            subTitleEl.className = 'premiere-subtitle';
            subTitleEl.textContent = airing.subTitle;
            item.appendChild(subTitleEl);
        }

        if (airing.description) {
            const descEl = document.createElement('div');
            descEl.className = 'premiere-description';
            descEl.textContent = airing.description;
            item.appendChild(descEl);
        }

        list.appendChild(item);
    }

    container.appendChild(list);
}

function getRoundedTime(date) {
    const h = date.getHours();
    const m = date.getMinutes() < 30 ? 0 : 30;
    return `${String(h).padStart(2, '0')}:${String(m).padStart(2, '0')}`;
}

async function renderTimeSlotMode(container) {
    const params = new URLSearchParams(window.location.search);
    const now = new Date();

    const selectedDate = params.get('date') || getTodayString();
    const selectedTime = params.get('time') || getRoundedTime(now);

    // Controls
    const controls = document.createElement('div');
    controls.className = 'time-slot-controls';

    const dateInput = document.createElement('input');
    dateInput.type = 'date';
    dateInput.className = 'time-slot-date-input';
    dateInput.value = selectedDate;
    controls.appendChild(dateInput);

    const timeSelect = document.createElement('select');
    timeSelect.className = 'time-slot-time-select';
    for (let h = 0; h < 24; h++) {
        for (const m of [0, 30]) {
            const timeStr = `${String(h).padStart(2, '0')}:${String(m).padStart(2, '0')}`;
            const opt = document.createElement('option');
            opt.value = timeStr;
            opt.textContent = timeStr;
            if (timeStr === selectedTime) opt.selected = true;
            timeSelect.appendChild(opt);
        }
    }
    controls.appendChild(timeSelect);

    container.appendChild(controls);

    // Results area
    const results = document.createElement('div');
    results.className = 'time-slot-results';
    container.appendChild(results);

    let guideData = null;

    function updateURL(date, time) {
        const newParams = new URLSearchParams(window.location.search);
        newParams.set('mode', 'time-slot');
        newParams.set('date', date);
        newParams.set('time', time);
        history.replaceState({}, '', '/explore?' + newParams.toString());
    }

    function renderResults(date, time) {
        results.innerHTML = '';

        if (guideData === null) return;

        const [y, mo, d] = date.split('-').map(Number);
        const [th, tm] = time.split(':').map(Number);
        const selectedMs = new Date(y, mo - 1, d, th, tm, 0, 0).getTime();

        const list = document.createElement('div');
        list.className = 'time-slot-list';

        for (const channel of state.channels) {
            const row = document.createElement('div');
            row.className = 'time-slot-row';
            row.dataset.channelId = channel.id;

            const channelEl = document.createElement('div');
            channelEl.className = 'time-slot-channel';
            channelEl.textContent = channel.displayName;
            row.appendChild(channelEl);

            const airing = guideData.find(a =>
                a.channelId === channel.id &&
                new Date(a.start).getTime() <= selectedMs &&
                new Date(a.stop).getTime() > selectedMs
            );

            const showEl = document.createElement('div');
            showEl.className = 'time-slot-show';

            if (airing) {
                const titleEl = document.createElement('span');
                titleEl.className = 'time-slot-title';
                titleEl.textContent = airing.title;
                showEl.appendChild(titleEl);

                const timeEl = document.createElement('span');
                timeEl.className = 'time-slot-time';
                timeEl.textContent = `${formatTime(new Date(airing.start))} – ${formatTime(new Date(airing.stop))}`;
                showEl.appendChild(timeEl);
            } else {
                showEl.textContent = 'Nothing airing';
            }

            row.appendChild(showEl);
            list.appendChild(row);
        }

        results.appendChild(list);
    }

    async function fetchAndRender(date, time) {
        updateURL(date, time);

        results.innerHTML = '';
        const loading = document.createElement('div');
        loading.className = CLASS_LOADING;
        loading.textContent = 'Loading…';
        results.appendChild(loading);

        try {
            const needChannels = state.channels.length === 0;
            const [guideResult, channelData] = await Promise.all([
                fetch(`/api/guide?date=${date}`).then(r => {
                    if (!r.ok) throw new Error(`/api/guide returned ${r.status}`);
                    return r.json();
                }),
                needChannels ? fetchChannels() : Promise.resolve(state.channels),
            ]);
            guideData = guideResult;
            if (needChannels) state.channels = channelData;
        } catch {
            results.innerHTML = '';
            const error = document.createElement('div');
            error.className = CLASS_ERROR;
            error.textContent = 'Failed to load guide data. Please try again.';
            results.appendChild(error);
            return;
        }

        renderResults(date, time);
    }

    dateInput.addEventListener('change', () => {
        fetchAndRender(dateInput.value, timeSelect.value);
    });

    timeSelect.addEventListener('change', () => {
        updateURL(dateInput.value, timeSelect.value);
        renderResults(dateInput.value, timeSelect.value);
    });

    await fetchAndRender(selectedDate, selectedTime);
}

async function renderNowNextMode(container) {
    const loading = document.createElement('div');
    loading.className = CLASS_LOADING;
    loading.textContent = 'Loading…';
    container.appendChild(loading);

    let entries;
    try {
        const res = await fetch('/api/explore/now-next');
        if (!res.ok) throw new Error(`/api/explore/now-next returned ${res.status}`);
        entries = await res.json();
    } catch {
        container.innerHTML = '';
        const error = document.createElement('div');
        error.className = CLASS_ERROR;
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
