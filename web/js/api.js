// ── API fetch wrappers ────────────────────────────────────────────────────────

import { state } from './state.js';

export async function fetchChannels() {
    const res = await fetch('/api/channels');
    if (!res.ok) throw new Error(`/api/channels returned ${res.status}`);
    return res.json();
}

export async function fetchGuide(dateStr) {
    const res = await fetch(`/api/guide?date=${dateStr}`);
    if (!res.ok) throw new Error(`/api/guide returned ${res.status}`);
    return res.json();
}

export async function fetchCategories() {
    if (state.categories.length > 0) return state.categories;
    const res = await fetch('/api/categories');
    if (!res.ok) throw new Error(`/api/categories returned ${res.status}`);
    state.categories = await res.json();
    return state.categories;
}

export function logError({ type, message, source, lineno, colno, stack, url }) {
    fetch('/api/debug/log', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ type, message, source, lineno, colno, stack, url }),
    }).catch(() => {}); // never throw from the error logger
}
