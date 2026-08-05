// Tile sources for the walkers basemap binding (ADR-0056).
//
// imzero2 fetches every basemap tile through BasemapTiles, its own
// implementation of walkers' public `Tiles` trait — walkers' own `HttpTiles`
// is not used. Two reasons, one forced and one chosen.
//
// Forced: walkers 0.56 offers no seam for TLS configuration. `HttpTiles` builds
// its reqwest client internally from `HttpOptions`, which carries cache, user
// agent and parallelism and nothing about certificates; the Fetch / TilesIo /
// TileFactory types that would let us supply a client are private to the crate.
// The client it builds trusts the webpki root bundle and only that — there is
// no system trust store to install a private CA into and SSL_CERT_FILE is not
// consulted — so an https tile server behind an internal CA is unreachable
// however valid its certificate.
//
// Chosen: one client rather than two selected by an env knob. The deciding
// argument was operability. walkers reports through the `log` crate, and every
// one of its diagnostics — a failed fetch, a dead IO thread, an undecodable
// response — was being dropped, so the default tile path could fail and leave
// nothing but a grey map behind. main.rs now bridges `log` into tracing, which
// fixes the silence; keeping one client is what stops the two paths drifting
// again in timeout, retry, proxy, user-agent and request-logging behaviour,
// and leaves one call site for the transport swap below.
//
// Behaviour still mirrors `HttpTiles` deliberately: a 256-entry LRU per map,
// interpolation from lower zoom levels while a tile is in flight, and a None
// cache entry written at request time that doubles as the in-flight marker and
// as the negative cache for a fetch that failed.
//
// Downloads run on ONE process-wide pool, not per map. walkers gives each
// HttpTiles its own runtime and its own 6-download budget, so two maps meant
// twelve concurrent connections and the cap meant nothing at the only level
// anyone rate-limits at — the client. The pool makes MAX_PARALLEL_DOWNLOADS a
// real process-wide bound, and costs a fixed six threads however many maps a
// window ends up with.
//
// deferred: the download half sits behind TileTransport because it should not
// stay in this process. Routing tile requests over FFFI2 into Go — which
// already owns the env registry, the proxy and TLS policy, and the capability
// gate — would leave one egress point instead of two independently configured
// ones, and would let non-HTTP tile sources (an on-disk mbtiles, tiles out of a
// database) drop in behind the same trait. See ADR-0165. The cache, the
// interpolation and the entire Go-side surface are unaffected by that swap.

use std::num::NonZeroUsize;
use std::sync::mpsc::{Receiver, Sender, SyncSender, TrySendError};
use std::sync::{Arc, Mutex, OnceLock};

use lru::LruCache;
use walkers::sources::{Attribution, TileSource};
use walkers::{Tile, TileId, TilePiece, Tiles};

// Cache size matches walkers'. MAX_PARALLEL_DOWNLOADS also matches, but means
// something stronger here: walkers applies it per tile source, this pool
// applies it per process, which is the level a tile server actually rate-limits
// at. Six is what browsers allow per host and what walkers' own default is.
const TILE_CACHE_ENTRIES: usize = 256;
const MAX_PARALLEL_DOWNLOADS: usize = 6;
const TILE_REQUEST_TIMEOUT: std::time::Duration = std::time::Duration::from_secs(30);

// Custom tile source — Go-supplied XYZ URL template with optional
// attribution / max-zoom / tile-size overrides. The URL template must
// contain `{z}`, `{x}`, `{y}` placeholders (no `{s}` subdomain rotation
// in v1 — users who need load balancing can hard-code a single subdomain
// or we add a subdomains Vec in a later rev).
pub struct CustomTileSource {
    url_template: String,
    // walkers::sources::Attribution holds `&'static str`s; we leak the
    // runtime-supplied strings once at construction time (see
    // `make_custom_tile_source`) so long-lived apps with churning tile
    // configs will see small, bounded memory growth per new source.
    attribution_text: &'static str,
    attribution_url: &'static str,
    tile_size_: u32,
    max_zoom_: u8,
}

impl TileSource for CustomTileSource {
    fn tile_url(&self, tile_id: TileId) -> String {
        // String::replace is cheap enough at tile-request rates; if this
        // becomes hot we can precompile the template into a Vec<Segment>.
        self.url_template
            .replace("{z}", &tile_id.zoom.to_string())
            .replace("{x}", &tile_id.x.to_string())
            .replace("{y}", &tile_id.y.to_string())
    }
    fn attribution(&self) -> Attribution {
        Attribution {
            text: self.attribution_text,
            url: self.attribution_url,
            logo_light: None,
            logo_dark: None,
        }
    }
    fn tile_size(&self) -> u32 {
        self.tile_size_
    }
    fn max_zoom(&self) -> u8 {
        self.max_zoom_
    }
}

fn leak_str(s: &str) -> &'static str {
    // Interning via a single static map would dedupe identical attributions;
    // for v1 the unbounded leak is acceptable (tile config changes at most
    // a handful of times in a realistic session). Revisit if it shows up
    // in a heap profile.
    Box::leak(s.to_owned().into_boxed_str())
}

pub fn make_custom_tile_source(
    url_template: String,
    attribution: &str,
    attribution_url: &str,
    tile_size: u32,
    max_zoom: u8,
) -> CustomTileSource {
    CustomTileSource {
        url_template,
        attribution_text: leak_str(attribution),
        attribution_url: leak_str(attribution_url),
        tile_size_: if tile_size == 0 { 256 } else { tile_size },
        max_zoom_: if max_zoom == 0 { 19 } else { max_zoom },
    }
}

/// How tile requests should treat the server's certificate.
///
/// Both fields come straight from Go (`BOXER_MAP_TILE_CA_FILE` /
/// `BOXER_MAP_TILE_INSECURE_TLS` via the `walkersMap` opcode) and are
/// meaningful only for a custom `.TileUrl` source.
#[derive(Clone, Default)]
pub struct TileTlsConfig {
    /// Path — on this process's filesystem — to a PEM bundle added to the
    /// trust roots. Verification stays on. Empty means "bundled roots only".
    ///
    /// It has to hold the issuing CA. A bare self-signed server certificate
    /// is not accepted as its own trust anchor, however valid it looks; that
    /// case needs `insecure`.
    pub ca_file: String,
    /// Accept any certificate. Supersedes `ca_file`.
    pub insecure: bool,
}

impl TileTlsConfig {
    /// True when either knob is set. Used to report a configuration that
    /// cannot apply — TLS settings reaching the built-in source, which Go
    /// gates against but a direct `walkersMap` caller could still do.
    pub fn is_configured(&self) -> bool {
        self.insecure || !self.ca_file.is_empty()
    }
}

/// Where tile bytes come from, decoded and ready to draw.
///
/// Decoding is the implementation's job, and must happen off the render
/// thread: a screenful of tiles landing at once is 20-40 PNG decodes, which is
/// a visible hitch if it runs inside [`Tiles::at`]. The HTTP implementation
/// below decodes in its download workers, the way walkers does.
pub trait TileTransport: Send {
    /// Queue a download. Returns false when the transport is saturated and
    /// nothing was queued — the caller leaves the tile uncached and asks
    /// again next frame, which is how walkers applies backpressure too.
    fn request(&mut self, tile_id: TileId) -> bool;

    /// Take one completed tile, or None if none is ready. Never blocks.
    /// A failed download is simply never delivered; the caller's cache
    /// entry stays empty, matching walkers' "empty forever" semantics.
    fn poll(&mut self) -> Option<(TileId, Tile)>;
}

/// One unit of work for the shared pool: everything a worker needs to fetch
/// and decode a tile without knowing which map asked.
struct TileJob {
    tile_id: TileId,
    url: String,
    /// Cloned per job — `reqwest::blocking::Client` is an `Arc` inside, and the
    /// per-map client is what carries that map's certificate policy.
    client: reqwest::blocking::Client,
    ctx: egui::Context,
    reply: Sender<(TileId, Tile)>,
}

/// The process-wide download pool. Started on first use and never shut down;
/// the threads park on an empty queue, so an app that opens no map pays for
/// nothing.
struct TilePool {
    submit: SyncSender<TileJob>,
}

static TILE_POOL: OnceLock<TilePool> = OnceLock::new();

fn tile_pool() -> &'static TilePool {
    TILE_POOL.get_or_init(|| {
        // Bounded at the worker count, like walkers' request channel: a full
        // queue means "already downloading as much as we should", and the
        // rejected tile is re-requested next frame, by which time the camera
        // may have moved and made it irrelevant. Unbounded queueing would spend
        // the whole download budget on tiles nobody is looking at after a pan.
        let (submit, jobs) = std::sync::mpsc::sync_channel::<TileJob>(MAX_PARALLEL_DOWNLOADS);
        // One receiver shared by all workers: whoever is free takes the next
        // job. The lock is held only across `recv`, so idle workers block on the
        // mutex and never while downloading.
        let jobs = Arc::new(Mutex::new(jobs));
        for i in 0..MAX_PARALLEL_DOWNLOADS {
            let jobs = Arc::clone(&jobs);
            let spawned = std::thread::Builder::new()
                .name(format!("imzero2-tiles-{i}"))
                .spawn(move || download_worker(&jobs));
            if let Err(e) = spawned {
                tracing::error!(error = %e, worker = i, "could not spawn a tile download worker");
            }
        }
        // Once per process, however many maps open. A second line here would
        // mean the pool had stopped being shared, which is the property the
        // download budget depends on.
        tracing::info!(
            workers = MAX_PARALLEL_DOWNLOADS,
            "started the tile download pool"
        );
        TilePool { submit }
    })
}

/// Downloads tiles over HTTP with a client this process configures.
///
/// The client is per map, so the certificate policy in [`TileTlsConfig`] can be
/// applied; the threads are not. This owns none — it hands jobs to the shared
/// pool and collects finished tiles on its own channel.
pub struct HttpTransport {
    /// `None` when the client could not be built. Every request then reports
    /// "not queued" and the map draws empty, rather than silently falling back
    /// to an unconfigured client the operator did not ask for.
    client: Option<reqwest::blocking::Client>,
    source: Arc<dyn TileSource + Send + Sync>,
    ctx: egui::Context,
    reply_tx: Sender<(TileId, Tile)>,
    reply_rx: Receiver<(TileId, Tile)>,
}

impl HttpTransport {
    pub fn new(
        source: Arc<dyn TileSource + Send + Sync>,
        tls: &TileTlsConfig,
        egui_ctx: &egui::Context,
    ) -> Self {
        let client = match build_client(tls) {
            Ok(client) => Some(client),
            Err(e) => {
                tracing::error!(error = %e, "tile HTTP client could not be built; this map will show no tiles");
                None
            }
        };
        let (reply_tx, reply_rx) = std::sync::mpsc::channel();
        Self {
            client,
            source,
            ctx: egui_ctx.clone(),
            reply_tx,
            reply_rx,
        }
    }
}

impl TileTransport for HttpTransport {
    fn request(&mut self, tile_id: TileId) -> bool {
        let Some(client) = &self.client else {
            return false;
        };
        let job = TileJob {
            tile_id,
            url: self.source.tile_url(tile_id),
            client: client.clone(),
            ctx: self.ctx.clone(),
            reply: self.reply_tx.clone(),
        };
        match tile_pool().submit.try_send(job) {
            Ok(()) => true,
            // Full: the process is already downloading as much as it should.
            // Disconnected: every worker is gone. Both answer "not queued", so
            // nothing is cached as pending; neither panics inside a render loop
            // the way walkers' own send-failure path does.
            Err(TrySendError::Full(_) | TrySendError::Disconnected(_)) => false,
        }
    }

    fn poll(&mut self) -> Option<(TileId, Tile)> {
        self.reply_rx.try_recv().ok()
    }
}

fn build_client(tls: &TileTlsConfig) -> reqwest::Result<reqwest::blocking::Client> {
    let mut builder = reqwest::blocking::Client::builder()
        .user_agent(concat!("boxer-imzero2/", env!("CARGO_PKG_VERSION")))
        .timeout(TILE_REQUEST_TIMEOUT);

    if tls.insecure {
        builder = builder.danger_accept_invalid_certs(true);
    } else if !tls.ca_file.is_empty() {
        // Read once per client, i.e. once per tile-config change — never per
        // request and never per frame. A file we cannot read or parse leaves
        // the bundled roots as they were: the connection then fails on an
        // untrusted certificate, which is the safe outcome for a misconfigured
        // path. Silently continuing with verification off would not be.
        match std::fs::read(&tls.ca_file) {
            Ok(pem) => match reqwest::Certificate::from_pem_bundle(&pem) {
                Ok(certs) => {
                    tracing::info!(
                        ca_file = %tls.ca_file,
                        count = certs.len(),
                        "added tile-server CA certificates to the trust roots"
                    );
                    for cert in certs {
                        builder = builder.add_root_certificate(cert);
                    }
                }
                Err(e) => tracing::error!(
                    ca_file = %tls.ca_file,
                    error = %e,
                    "tile-server CA bundle is not readable PEM; keeping the built-in roots"
                ),
            },
            Err(e) => tracing::error!(
                ca_file = %tls.ca_file,
                error = %e,
                "tile-server CA bundle could not be read; keeping the built-in roots"
            ),
        }
    }

    builder.build()
}

/// Pool worker. Serves every map in the process, so a dead reply channel (its
/// map was dropped, or its tile config changed and it was rebuilt) skips the
/// job rather than ending the thread.
fn download_worker(jobs: &Mutex<Receiver<TileJob>>) {
    // `Style::default()` rather than `Style`: walkers' Style is a unit struct
    // only while its `mvt` feature is off, and this should not break the day
    // something turns it on.
    #[expect(
        clippy::default_constructed_unit_structs,
        reason = "walkers::Style is only a unit struct without the mvt feature"
    )]
    let style = walkers::Style::default();
    loop {
        let job = {
            let Ok(rx) = jobs.lock() else {
                // Poisoned by a panicking sibling; this worker is done.
                return;
            };
            match rx.recv() {
                Ok(job) => job,
                // The pool is static, so this only happens at process teardown.
                Err(_) => return,
            }
        };

        let body = job
            .client
            .get(&job.url)
            .send()
            .and_then(reqwest::blocking::Response::error_for_status)
            .and_then(reqwest::blocking::Response::bytes);
        let body = match body {
            Ok(body) => body,
            Err(e) => {
                tracing::warn!(url = %job.url, error = %e, "tile download failed");
                continue;
            }
        };

        // Decode and upload here rather than on the render thread — this is
        // the whole reason the pool exists and not just an async client.
        match Tile::new(&body, &style, job.tile_id.zoom, &job.ctx) {
            Ok(tile) => {
                if job.reply.send((job.tile_id, tile)).is_err() {
                    // That map is gone. Other maps still need this worker.
                    continue;
                }
                // Without this a still map never repaints and the tile only
                // appears the next time the user touches something.
                job.ctx.request_repaint();
            }
            Err(e) => tracing::warn!(url = %job.url, error = %e, "tile could not be decoded"),
        }
    }
}

/// The [`Tiles`] implementation every imzero2 basemap draws from, whatever its
/// source: the OpenStreetMap default, a custom XYZ template, verified or not.
pub struct BasemapTiles {
    attribution: Attribution,
    tile_size: u32,
    max_zoom: u8,
    // `Some(tile)` is drawable; `None` means requested and not here yet, or
    // requested and failed. walkers does not distinguish those two either.
    cache: LruCache<TileId, Option<Tile>>,
    transport: Box<dyn TileTransport>,
}

impl BasemapTiles {
    pub fn new(
        source: Arc<dyn TileSource + Send + Sync>,
        tls: &TileTlsConfig,
        egui_ctx: &egui::Context,
    ) -> Self {
        let attribution = source.attribution();
        let tile_size = source.tile_size();
        let max_zoom = source.max_zoom();
        let transport = Box::new(HttpTransport::new(source, tls, egui_ctx));
        Self {
            attribution,
            tile_size,
            max_zoom,
            cache: LruCache::new(
                NonZeroUsize::new(TILE_CACHE_ENTRIES).unwrap_or(NonZeroUsize::MIN),
            ),
            transport,
        }
    }

    /// Move one finished download into the cache. Called from every `at`,
    /// so a frame absorbs roughly as many tiles as it draws.
    fn take_one_fetched(&mut self) {
        if let Some((tile_id, tile)) = self.transport.poll() {
            self.cache.put(tile_id, Some(tile));
        }
    }

    /// Request `tile_id` unless it is already cached or already requested. A
    /// saturated pool leaves the cache untouched so the next frame asks again.
    fn make_sure_is_fetched(&mut self, tile_id: TileId) {
        if self.cache.contains(&tile_id) {
            return;
        }
        if self.transport.request(tile_id) {
            self.cache.put(tile_id, None);
        }
    }

    /// The drawable tile, or a magnified crop of the nearest lower zoom level
    /// that has arrived. Keeps a map legible while a zoom step downloads.
    fn get_from_cache_or_interpolate(&mut self, tile_id: TileId) -> Option<TilePiece> {
        let mut zoom_candidate = tile_id.zoom;
        loop {
            let (zoomed_tile_id, uv) = interpolate_from_lower_zoom(tile_id, zoom_candidate);
            if let Some(Some(tile)) = self.cache.get(&zoomed_tile_id) {
                break Some(TilePiece::new(tile.clone(), uv));
            }
            zoom_candidate = zoom_candidate.checked_sub(1)?;
        }
    }
}

impl Tiles for BasemapTiles {
    fn at(&mut self, tile_id: TileId) -> Option<TilePiece> {
        self.take_one_fetched();

        if !tile_id_is_valid(tile_id) {
            return None;
        }

        let tile_id_to_download = if tile_id.zoom > self.max_zoom {
            interpolate_from_lower_zoom(tile_id, self.max_zoom).0
        } else {
            tile_id
        };

        self.make_sure_is_fetched(tile_id_to_download);
        self.get_from_cache_or_interpolate(tile_id)
    }

    fn attribution(&self) -> Attribution {
        self.attribution.clone()
    }

    fn tile_size(&self) -> u32 {
        self.tile_size
    }
}

// walkers keeps `TileId::valid` and `interpolate_from_lower_zoom` crate-
// private, so the two are restated here. Both are plain slippy-map
// arithmetic; the saturating forms below differ from walkers only in not
// panicking above zoom 31, which `.TileMaxZoom(255)` can reach.

fn total_tiles(zoom: u8) -> u32 {
    1u32.checked_shl(u32::from(zoom)).unwrap_or(u32::MAX)
}

fn tile_id_is_valid(tile_id: TileId) -> bool {
    let total = total_tiles(tile_id.zoom);
    tile_id.x < total && tile_id.y < total
}

/// The tile at `available_zoom` that covers `tile_id`, plus the sub-rectangle
/// of it to draw.
fn interpolate_from_lower_zoom(tile_id: TileId, available_zoom: u8) -> (TileId, egui::Rect) {
    debug_assert!(
        tile_id.zoom >= available_zoom,
        "a donor tile must come from the same zoom level or lower"
    );

    let dzoom = 2u32
        .checked_pow(u32::from(tile_id.zoom.saturating_sub(available_zoom)))
        .unwrap_or(u32::MAX);

    let x = (tile_id.x / dzoom, tile_id.x % dzoom);
    let y = (tile_id.y / dzoom, tile_id.y % dzoom);

    let zoomed_tile_id = TileId {
        x: x.0,
        y: y.0,
        zoom: available_zoom,
    };

    let z = (dzoom as f32).recip();
    let uv = egui::Rect::from_min_max(
        egui::pos2(x.1 as f32 * z, y.1 as f32 * z),
        egui::pos2(x.1 as f32 * z + z, y.1 as f32 * z + z),
    );

    (zoomed_tile_id, uv)
}

#[cfg(test)]
mod tests {
    use super::*;

    // Pins the restated arithmetic against walkers' original: the tile one
    // zoom level up covers the requested tile, and the uv picks the correct
    // quadrant of it.
    #[test]
    fn interpolation_picks_the_covering_tile_and_quadrant() {
        let tile_id = TileId {
            x: 3,
            y: 2,
            zoom: 4,
        };
        let (donor, uv) = interpolate_from_lower_zoom(tile_id, 3);
        assert_eq!(
            donor,
            TileId {
                x: 1,
                y: 1,
                zoom: 3
            }
        );
        assert_eq!(uv.min, egui::pos2(0.5, 0.0));
        assert_eq!(uv.max, egui::pos2(1.0, 0.5));

        // Same zoom is the identity: the whole tile.
        let (donor, uv) = interpolate_from_lower_zoom(tile_id, 4);
        assert_eq!(donor, tile_id);
        assert_eq!(
            uv,
            egui::Rect::from_min_max(egui::pos2(0.0, 0.0), egui::pos2(1.0, 1.0))
        );
    }

    #[test]
    fn tile_validity_follows_the_zoom_level() {
        // One tile at zoom 0, four at zoom 1.
        assert!(tile_id_is_valid(TileId {
            x: 0,
            y: 0,
            zoom: 0
        }));
        assert!(!tile_id_is_valid(TileId {
            x: 1,
            y: 0,
            zoom: 0
        }));
        assert!(tile_id_is_valid(TileId {
            x: 1,
            y: 1,
            zoom: 1
        }));
        assert!(!tile_id_is_valid(TileId {
            x: 2,
            y: 0,
            zoom: 1
        }));

        // Past zoom 31 walkers' own `2u32.pow` overflows; `.TileMaxZoom(255)`
        // is reachable from the IDL, so this saturates instead.
        assert!(tile_id_is_valid(TileId {
            x: u32::MAX - 1,
            y: 0,
            zoom: 255
        }));
    }

    #[test]
    fn tls_config_is_only_configured_when_a_knob_is_set() {
        assert!(!TileTlsConfig::default().is_configured());
        assert!(
            TileTlsConfig {
                ca_file: "/etc/ssl/gis-ca.pem".to_owned(),
                insecure: false,
            }
            .is_configured()
        );
        assert!(
            TileTlsConfig {
                ca_file: String::new(),
                insecure: true,
            }
            .is_configured()
        );
    }
}
