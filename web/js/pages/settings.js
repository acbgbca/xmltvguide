import { state } from '../state.js';
import { isHidden, isFavourite, toggleHidden, toggleFavourite } from '../store/preferences.js';
import { fetchChannels, fetchGuide, refreshGuide } from '../api.js';
import { renderGuide, hasAiringsStartingOn } from './guide.js';

export function renderSettingsPanel() {
    const panel = document.getElementById('settingsPanel');
    panel.innerHTML = '';

    panel.appendChild(buildChannelsSection());
    panel.appendChild(buildAdvancedSection());
}

function buildChannelsSection() {
    const section = document.createElement('section');
    section.className = 'settings-section channels-section';

    const heading = document.createElement('h1');
    heading.className   = 'settings-section-heading';
    heading.textContent = 'Channels';
    section.appendChild(heading);

    const list = document.createElement('div');
    list.className = 'settings-list';
    list.id        = 'settingsList';
    populateChannelsList(list);

    section.appendChild(list);
    return section;
}

function populateChannelsList(list) {
    list.innerHTML = '';
    for (const ch of state.channels) {
        const hidden = isHidden(ch.id);
        const fav    = isFavourite(ch.id);

        const item = document.createElement('div');
        item.className = 'settings-item' + (hidden ? ' is-hidden' : '');

        const favBtn = document.createElement('button');
        favBtn.className = 'settings-icon-btn fav-btn' + (fav ? ' active' : '');
        favBtn.textContent = '★'; // ★
        favBtn.title = fav ? 'Remove from favourites' : 'Add to favourites';
        favBtn.addEventListener('click', () => {
            toggleFavourite(ch.id);
            renderGuide();
            populateChannelsList(list);
        });

        const name = document.createElement('span');
        name.className   = 'settings-name';
        name.textContent = ch.displayName;

        const hideBtn = document.createElement('button');
        hideBtn.className = 'settings-icon-btn hide-btn' + (hidden ? ' active' : '');
        hideBtn.textContent = hidden ? '\u{1F648}' : '\u{1F441}'; // 🙈 / 👁
        hideBtn.title = hidden ? 'Show channel' : 'Hide channel';
        hideBtn.addEventListener('click', () => {
            toggleHidden(ch.id);
            renderGuide();
            populateChannelsList(list);
        });

        item.appendChild(favBtn);
        item.appendChild(name);
        item.appendChild(hideBtn);
        list.appendChild(item);
    }
}

function buildAdvancedSection() {
    const section = document.createElement('section');
    section.className = 'settings-accordion advanced-section';

    const header = document.createElement('button');
    header.type      = 'button';
    header.className = 'settings-accordion-header';
    header.setAttribute('aria-expanded', 'false');
    header.innerHTML =
        '<span class="settings-accordion-title">Advanced</span>' +
        '<span class="settings-accordion-chevron" aria-hidden="true">▾</span>';

    const body = document.createElement('div');
    body.className = 'settings-accordion-body';
    body.appendChild(buildRefreshAction());

    header.addEventListener('click', () => {
        const expanded = section.classList.toggle('is-expanded');
        header.setAttribute('aria-expanded', expanded ? 'true' : 'false');
    });

    section.appendChild(header);
    section.appendChild(body);
    return section;
}

function buildRefreshAction() {
    const wrap = document.createElement('div');
    wrap.className = 'settings-action settings-action-refresh-wrap';

    const description = document.createElement('p');
    description.className   = 'settings-action-desc';
    description.textContent = 'Re-fetches the XMLTV source now.';

    const row = document.createElement('div');
    row.className = 'settings-action-row';

    const btn = document.createElement('button');
    btn.type        = 'button';
    btn.className   = 'settings-action-btn settings-action-refresh';
    btn.textContent = 'Refresh guide';

    const spinner = document.createElement('div');
    spinner.className = 'loading-spinner';
    spinner.style.display = 'none';

    const status = document.createElement('div');
    status.className = 'settings-status';
    status.style.display = 'none';
    status.setAttribute('role', 'status');

    btn.addEventListener('click', async () => {
        btn.disabled = true;
        spinner.style.display = '';
        hideStatus(status);

        try {
            await refreshGuide();
            const [channels, programmes] = await Promise.all([
                fetchChannels(),
                fetchGuide(state.currentDate),
            ]);
            state.channels   = channels;
            state.programmes = programmes;

            const hasData = hasAiringsStartingOn(state.programmes, state.currentDate);
            const empty = document.getElementById('guideEmpty');
            const container = document.querySelector('.guide-container');
            if (empty)     empty.classList.toggle('visible', !hasData);
            if (container) container.classList.toggle('no-data', !hasData);
            renderGuide();

            const list = document.getElementById('settingsList');
            if (list) populateChannelsList(list);

            showStatus(status, 'Guide refreshed', 'is-success');
        } catch (err) {
            showStatus(status, err.message || 'Refresh failed', 'is-error');
        } finally {
            btn.disabled = false;
            spinner.style.display = 'none';
        }
    });

    row.appendChild(btn);
    row.appendChild(spinner);
    row.appendChild(status);

    wrap.appendChild(description);
    wrap.appendChild(row);
    return wrap;
}

function showStatus(el, text, cls) {
    el.textContent = text;
    el.className = `settings-status ${cls}`;
    el.style.display = '';
}

function hideStatus(el) {
    el.style.display = 'none';
    el.className = 'settings-status';
    el.textContent = '';
}
