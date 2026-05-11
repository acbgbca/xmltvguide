import { state } from '../state.js';
import { isHidden, isFavourite, toggleHidden, toggleFavourite } from '../store/preferences.js';
import { fetchChannels, fetchGuide, fetchStatus, refreshGuide, runDeepCheck } from '../api.js';
import { renderGuide, hasAiringsStartingOn } from './guide.js';
import { formatAbsoluteDateTime } from '../utils/date.js';

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
    const statusBlock = buildStatusBlock();
    body.appendChild(statusBlock);
    const deepCheck = buildDeepCheckAction();
    body.appendChild(deepCheck.wrap);
    body.appendChild(buildResetAction());

    let statusLoaded = false;
    header.addEventListener('click', () => {
        const expanded = section.classList.toggle('is-expanded');
        header.setAttribute('aria-expanded', expanded ? 'true' : 'false');
        if (expanded && !statusLoaded) {
            statusLoaded = true;
            loadStatus(statusBlock);
        }
        if (!expanded) {
            deepCheck.clear();
        }
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

const DEEPCHECK_LABELS = {
    database:          'Database',
    database_writable: 'Database writable',
    fts:               'FTS index',
    data_presence:     'Data presence',
    data_freshness:    'Data freshness',
    xmltv_url:         'XMLTV source',
    disk_data:         'Disk: /data',
    disk_tmp:          'Disk: /tmp',
    image_cache:       'Image cache',
};

function humaniseDeepCheckName(name) {
    return DEEPCHECK_LABELS[name] || name;
}

function buildDeepCheckAction() {
    const wrap = document.createElement('div');
    wrap.className = 'settings-action settings-action-deepcheck-wrap';

    const description = document.createElement('p');
    description.className   = 'settings-action-desc';
    description.textContent = 'Runs a deeper health check across database, storage, network, and data freshness.';

    const row = document.createElement('div');
    row.className = 'settings-action-row';

    const btn = document.createElement('button');
    btn.type        = 'button';
    btn.className   = 'settings-action-btn settings-action-deepcheck';
    btn.textContent = 'Run system check';

    const spinner = document.createElement('div');
    spinner.className = 'loading-spinner';
    spinner.style.display = 'none';

    row.appendChild(btn);
    row.appendChild(spinner);

    const results = document.createElement('div');
    results.className = 'settings-deepcheck-results';
    results.style.display = 'none';
    results.setAttribute('role', 'status');

    const clear = () => {
        results.innerHTML = '';
        results.style.display = 'none';
    };

    btn.addEventListener('click', async () => {
        btn.disabled = true;
        spinner.style.display = '';
        clear();

        let report;
        try {
            report = await runDeepCheck();
        } catch (err) {
            renderDeepCheckNetworkError(results, err);
            btn.disabled = false;
            spinner.style.display = 'none';
            return;
        }

        renderDeepCheckReport(results, report);
        btn.disabled = false;
        spinner.style.display = 'none';
    });

    wrap.appendChild(description);
    wrap.appendChild(row);
    wrap.appendChild(results);

    return { wrap, clear };
}

function renderDeepCheckReport(results, report) {
    results.innerHTML = '';
    results.style.display = '';

    const checks = Array.isArray(report.checks) ? report.checks : [];
    const failed = checks.filter((c) => c.status !== 'SUCCESS').length;
    const total  = checks.length;

    const summary = document.createElement('div');
    summary.className = 'settings-deepcheck-summary';
    if (report.status === 'SUCCESS' && failed === 0) {
        summary.classList.add('is-success');
        summary.textContent = 'All checks passed';
    } else {
        summary.classList.add('is-error');
        summary.textContent = `${failed} of ${total} checks failed`;
    }
    results.appendChild(summary);

    for (const check of checks) {
        results.appendChild(buildDeepCheckRow(check));
    }
}

function buildDeepCheckRow(check) {
    const row = document.createElement('div');
    row.className = 'settings-deepcheck-row';

    const head = document.createElement('div');
    head.className = 'settings-deepcheck-head';

    const success = check.status === 'SUCCESS';

    const icon = document.createElement('span');
    icon.className = 'settings-deepcheck-icon ' + (success ? 'is-success' : 'is-error');
    icon.textContent = success ? '✓' : '✗'; // ✓ / ✗
    icon.setAttribute('aria-hidden', 'true');

    const name = document.createElement('span');
    name.className   = 'settings-deepcheck-name';
    name.textContent = humaniseDeepCheckName(check.name);

    head.appendChild(icon);
    head.appendChild(name);

    if (check.info) {
        const info = document.createElement('span');
        info.className   = 'settings-deepcheck-info';
        info.textContent = check.info;
        head.appendChild(info);
    }

    row.appendChild(head);

    if (check.error) {
        const err = document.createElement('div');
        err.className   = 'settings-deepcheck-error';
        err.textContent = check.error;
        row.appendChild(err);
    }

    return row;
}

function renderDeepCheckNetworkError(results, err) {
    results.innerHTML = '';
    results.style.display = '';

    const row = document.createElement('div');
    row.className = 'settings-deepcheck-row';

    const head = document.createElement('div');
    head.className = 'settings-deepcheck-head';

    const icon = document.createElement('span');
    icon.className = 'settings-deepcheck-icon is-error';
    icon.textContent = '✗';
    icon.setAttribute('aria-hidden', 'true');

    const name = document.createElement('span');
    name.className   = 'settings-deepcheck-name';
    name.textContent = `System check could not be run: ${err.message || 'request failed'}`;

    head.appendChild(icon);
    head.appendChild(name);
    row.appendChild(head);

    results.appendChild(row);
}

function buildStatusBlock() {
    const wrap = document.createElement('div');
    wrap.className = 'settings-status-block';
    wrap.setAttribute('aria-busy', 'true');

    const loading = document.createElement('div');
    loading.className   = 'settings-status-loading';
    loading.textContent = 'Loading status…';
    wrap.appendChild(loading);

    return wrap;
}

async function loadStatus(block) {
    try {
        const status = await fetchStatus();
        renderStatus(block, status);
    } catch {
        renderStatusUnavailable(block);
    } finally {
        block.removeAttribute('aria-busy');
    }
}

function renderStatus(block, status) {
    block.innerHTML = '';
    block.appendChild(buildStatusRow('Last refresh', formatStatusTime(status.lastRefresh)));
    block.appendChild(buildStatusRow('Next refresh', formatStatusTime(status.nextRefresh)));
    block.appendChild(buildStatusRow('Source', status.sourceUrl || '—', { url: true, title: status.sourceUrl }));
}

function buildStatusRow(label, value, opts = {}) {
    const row = document.createElement('div');
    row.className = 'settings-status-row';

    const l = document.createElement('span');
    l.className   = 'settings-status-label';
    l.textContent = label;

    const v = document.createElement('span');
    v.className   = 'settings-status-value' + (opts.url ? ' settings-status-value-url' : '');
    v.textContent = value;
    if (opts.title) v.title = opts.title;

    row.appendChild(l);
    row.appendChild(v);
    return row;
}

function formatStatusTime(value) {
    if (!value) return '—';
    const d = new Date(value);
    if (isNaN(d.getTime())) return '—';
    return formatAbsoluteDateTime(d);
}

function renderStatusUnavailable(block) {
    block.innerHTML = '';
    const msg = document.createElement('div');
    msg.className   = 'settings-status-unavailable';
    msg.textContent = 'Status unavailable';
    block.appendChild(msg);
}

function buildResetAction() {
    const wrap = document.createElement('div');
    wrap.className = 'settings-action settings-action-reset-wrap';

    const description = document.createElement('p');
    description.className   = 'settings-action-desc';
    description.textContent = 'Clears hidden channels, channel favourites, and saved searches stored on this device.';

    const row = document.createElement('div');
    row.className = 'settings-action-row';

    const btn = document.createElement('button');
    btn.type        = 'button';
    btn.className   = 'settings-action-btn settings-action-reset';
    btn.textContent = 'Reset preferences';

    btn.addEventListener('click', () => {
        if (!window.confirm('Reset all preferences? This cannot be undone.')) return;
        localStorage.removeItem('tvguide-prefs');
        localStorage.removeItem('tvguide-favourites');
        window.location.reload();
    });

    row.appendChild(btn);
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
