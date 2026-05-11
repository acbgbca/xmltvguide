// ── API fetch wrappers ────────────────────────────────────────────────────────

import { state } from './state.js';

// handleRedirect detects when Traefik/Authelia has redirected the request to a
// login page. fetch() with redirect:'manual' returns an opaque redirect instead
// of following a cross-origin redirect, so we trigger a full page navigation
// which lets the browser follow the redirect chain to the login page and back.
function handleRedirect(res) {
    if (res.type === 'opaqueredirect') {
        const el = document.getElementById('loadingText');
        if (el) el.textContent = 'Redirecting to authentication...';
        window.location.replace(window.location.href);
        return new Promise(() => {}); // never resolves — navigation is in progress
    }
    return res;
}

export async function fetchChannels() {
    const res = await fetch('/api/channels', { redirect: 'manual' }).then(handleRedirect);
    if (!res.ok) throw new Error(`/api/channels returned ${res.status}`);
    return res.json();
}

export async function fetchGuide(dateStr) {
    const res = await fetch(`/api/guide?date=${dateStr}`, { redirect: 'manual' }).then(handleRedirect);
    if (!res.ok) throw new Error(`/api/guide returned ${res.status}`);
    return res.json();
}

export async function fetchCategories() {
    if (state.categories.length > 0) return state.categories;
    const res = await fetch('/api/categories', { redirect: 'manual' }).then(handleRedirect);
    if (!res.ok) throw new Error(`/api/categories returned ${res.status}`);
    state.categories = await res.json();
    return state.categories;
}

export async function fetchStatus() {
    const res = await fetch('/api/status', { redirect: 'manual' }).then(handleRedirect);
    if (!res.ok) throw new Error(`/api/status returned ${res.status}`);
    return res.json();
}

export async function refreshGuide() {
    const res = await fetch('/api/guide/refresh?sync=true', {
        method: 'POST',
        redirect: 'manual',
    }).then(handleRedirect);
    if (!res.ok) {
        let msg = `Refresh failed (${res.status})`;
        try {
            const body = await res.json();
            if (body && body.error) msg = body.error;
        } catch { /* response had no JSON body */ }
        throw new Error(msg);
    }
    return res.json();
}

export function logError({ type, message, source, lineno, colno, stack, url }) {
    fetch('/api/debug/log', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ type, message, source, lineno, colno, stack, url }),
    }).catch(() => {}); // never throw from the error logger
}
