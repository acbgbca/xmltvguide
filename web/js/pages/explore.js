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
    content.innerHTML = '<p>Coming soon</p>';
    container.appendChild(content);
}
