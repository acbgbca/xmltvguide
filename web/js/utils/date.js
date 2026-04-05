// ── Date / time utilities ─────────────────────────────────────────────────────

export function getTodayString() {
    const now = new Date();
    const y = now.getFullYear();
    const m = String(now.getMonth() + 1).padStart(2, '0');
    const d = String(now.getDate()).padStart(2, '0');
    return `${y}-${m}-${d}`;
}

// Returns a Date representing midnight local time for a 'YYYY-MM-DD' string.
export function dateMidnight(dateStr) {
    const [y, m, d] = dateStr.split('-').map(Number);
    return new Date(y, m - 1, d, 0, 0, 0, 0);
}

export function formatTime(date) {
    return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', hour12: false });
}

export function formatDateLong(dateStr) {
    const [y, m, d] = dateStr.split('-').map(Number);
    return new Date(y, m - 1, d).toLocaleDateString([], {
        weekday: 'short', day: 'numeric', month: 'short',
    });
}

// Formats a minute-offset-from-midnight as "HH:MM".
export function minutesToHHMM(minutes) {
    const h = Math.floor(minutes / 60) % 24;
    const m = minutes % 60;
    return `${String(h).padStart(2, '0')}:${String(m).padStart(2, '0')}`;
}

// Returns a new date string offset by `days` from the given 'YYYY-MM-DD' string.
export function addDays(dateStr, days) {
    const [y, m, d] = dateStr.split('-').map(Number);
    const date = new Date(y, m - 1, d + days);
    return [
        date.getFullYear(),
        String(date.getMonth() + 1).padStart(2, '0'),
        String(date.getDate()).padStart(2, '0'),
    ].join('-');
}

export function formatSearchDate(date) {
    const now = new Date();
    const today = new Date(now.getFullYear(), now.getMonth(), now.getDate());
    const tomorrow = new Date(today);
    tomorrow.setDate(tomorrow.getDate() + 1);
    const weekAhead = new Date(today);
    weekAhead.setDate(weekAhead.getDate() + 7);

    const dateOnly = new Date(date.getFullYear(), date.getMonth(), date.getDate());
    const time = date.toLocaleTimeString([], { hour: 'numeric', minute: '2-digit', hour12: true });

    if (dateOnly.getTime() === today.getTime()) {
        return 'Today ' + time;
    }
    if (dateOnly.getTime() === tomorrow.getTime()) {
        return 'Tomorrow ' + time;
    }
    if (dateOnly < weekAhead) {
        return date.toLocaleDateString([], { weekday: 'short' }) + ' ' + time;
    }
    return date.toLocaleDateString([], { weekday: 'short', day: 'numeric', month: 'short' }) + ' ' + time;
}
