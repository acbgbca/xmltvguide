import { formatTime } from '../utils/date.js';

// ── Programme details modal ───────────────────────────────────────────────────

export function openProgrammeModal(prog) {
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

    // Watch now: hide buttons until we hear back from the backend so a previous
    // airing's URLs can't leak into one without a Plex mapping.
    resetWatchNow();
    if (prog.channelId) {
        updateWatchNow(prog.channelId);
    }

    document.getElementById('modalBackdrop').classList.add('open');
}

export function openSearchAiringModal(airing, title) {
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
        channelId:         airing.channelId,
    };
    openProgrammeModal(prog);
}

export function closeModal() {
    document.getElementById('modalBackdrop').classList.remove('open');
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

function resetWatchNow() {
    const wrap = document.getElementById('modalWatchNow');
    wrap.style.display = 'none';
    document.getElementById('modalWatchWeb').setAttribute('href', '#');
    document.getElementById('modalWatchApp').setAttribute('href', '#');
}

async function updateWatchNow(channelId) {
    try {
        const resp = await fetch(`/api/channels/${encodeURIComponent(channelId)}/plex-link`);
        if (!resp.ok) return;
        const data = await resp.json();
        if (!data?.webUrl || !data?.appUrl) return;
        document.getElementById('modalWatchWeb').setAttribute('href', data.webUrl);
        document.getElementById('modalWatchApp').setAttribute('href', data.appUrl);
        document.getElementById('modalWatchNow').style.display = '';
    } catch {
        // Network errors silently hide the buttons — Watch now is non-critical.
    }
}
