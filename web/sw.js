const CACHE = 'tvguide-__CACHE_VERSION__';
const STATIC = ['/', '/index.html', '/style.css', '/js/main.js', '/js/config.js', '/js/utils/date.js', '/manifest.json', '/icon.svg', '/apple-touch-icon.png', '/icon-192.png', '/icon-512.png'];

// SPA routes that should be served from the cached index.html
const SPA_ROUTES = ['/guide', '/search', '/favourites', '/settings'];

self.addEventListener('install', event => {
    event.waitUntil(
        caches.open(CACHE).then(cache => cache.addAll(STATIC))
    );
    self.skipWaiting();
});

self.addEventListener('activate', event => {
    event.waitUntil(
        caches.keys().then(keys =>
            Promise.all(keys.filter(k => k !== CACHE).map(k => caches.delete(k)))
        )
    );
    self.clients.claim();
});

self.addEventListener('fetch', event => {
    const url = new URL(event.request.url);

    if (url.pathname.startsWith('/api/')) {
        // Network-first for API: always try to get fresh data.
        event.respondWith(
            fetch(event.request).catch(() => caches.match(event.request))
        );
    } else if (SPA_ROUTES.includes(url.pathname)) {
        // SPA routes: serve cached index.html (the SPA shell)
        event.respondWith(
            caches.match('/index.html').then(cached => cached || fetch('/'))
        );
    } else {
        // Cache-first for static assets.
        event.respondWith(
            caches.match(event.request).then(cached => cached || fetch(event.request))
        );
    }
});
