// RACK MONITOR BY DON — service worker: abre a tela mesmo com o servidor reiniciando.
const C = "rackdon-v2.12.1";
const SHELL = ["./", "index.html", "js/core.js", "js/operacao.js", "js/jobs.js", "js/show.js", "js/config.js", "js/inicio.js", "app.css", "logo.png", "icon-192.png"];
self.addEventListener("install", e => e.waitUntil(caches.open(C).then(c => c.addAll(SHELL)).then(() => self.skipWaiting())));
self.addEventListener("activate", e => e.waitUntil(caches.keys().then(k => Promise.all(k.filter(x => x !== C).map(x => caches.delete(x)))).then(() => self.clients.claim())));
self.addEventListener("fetch", e => {
  const u = new URL(e.request.url);
  if (e.request.method !== "GET" || u.pathname.startsWith("/api/") || u.pathname === "/sse") return; // dados sempre ao vivo
  e.respondWith(fetch(e.request).then(r => { const cp = r.clone(); caches.open(C).then(c => c.put(e.request, cp)); return r; }).catch(() => caches.match(e.request)));
});
