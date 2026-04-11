import { formatTime, formatSearchDate } from '../utils/date.js';
import { fetchCategories } from '../api.js';
import { openSearchAiringModal } from '../components/modal.js';

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
    } else {
        content.innerHTML = '<p>Coming soon</p>';
    }

    container.appendChild(content);
}

async function renderCategoriesMode(container) {
    const params = new URLSearchParams(window.location.search);
    const selectedCategory = params.get('category');

    if (selectedCategory) {
        await renderCategoryResults(container, selectedCategory);
    } else {
        await renderCategoryPicker(container);
    }
}

async function renderCategoryPicker(container) {
    const loading = document.createElement('div');
    loading.className = 'explore-loading';
    loading.textContent = 'Loading…';
    container.appendChild(loading);

    let categories;
    try {
        categories = await fetchCategories();
    } catch (err) {
        container.innerHTML = '';
        const error = document.createElement('div');
        error.className = 'explore-error';
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
    loading.className = 'explore-loading';
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
    } catch (err) {
        loading.remove();
        const error = document.createElement('div');
        error.className = 'explore-error';
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
