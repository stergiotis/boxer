// Tile sources for the walkers basemap binding (ADR-0056).
//
// Two things live here. CustomTileSource is the XYZ URL-template source Go
// configures through .TileUrl; walkers' own HttpTiles renders it whenever no
// TLS configuration is in play, and that path is untouched. CustomHttpTiles is
// a second Tiles implementation, reached only when Go asked for TLS
// configuration walkers cannot express.
//
// It exists because walkers 0.56 offers no seam for it. HttpTiles builds its
// reqwest client internally from HttpOptions, which carries cache, user agent
// and parallelism and nothing about certificates; the Fetch / TilesIo /
// TileFactory types that would let us supply a client are private to the
// crate. The client it builds trusts the webpki root bundle and only that —
// there is no system trust store to install a private CA into and SSL_CERT_FILE
// is not consulted — so an https tile server behind an internal CA is
// unreachable, whether or not its certificate is otherwise valid.
//
// Observable behaviour deliberately mirrors HttpTiles: a 256-entry LRU, six
// parallel downloads, interpolation from lower zoom levels when a tile has not
// arrived yet, and a None cache entry written at request time that doubles as
// the in-flight marker and as the negative cache for a fetch that failed.
// imzero2 constructs HttpTiles with default HttpOptions, so that is the whole
// of the behaviour there is to match.
//
// deferred: the download half sits behind TileTransport because it should not
// stay in this process. Routing tile requests over FFFI2 into Go — which
// already owns the env registry, the proxy and TLS policy, and the capability
// gate — would leave one egress point instead of two independently configured
// ones, and would let non-HTTP tile sources (an on-disk mbtiles, tiles out of a
// database) drop in behind the same trait. See ADR-0165. The cache, the
// interpolation and the entire Go-side surface are unaffected by that swap.

use std::num::NonZeroUsize;
use std::sync::mpsc::{Receiver, SyncSender, TrySendError};
use std::sync::{Arc, Mutex};

use lru::LruCache;
use walkers::sources::{Attribution, TileSource};
use walkers::{Tile, TileId, TilePiece, Tiles};

// Matches walkers' own cache size and parallelism, so a deployment that flips
// the TLS knobs on does not also silently change how hard it hits its server.
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
    tile_size: u32,
    max_zoom: u8,
) -> CustomTileSource {
    CustomTileSource {
        url_template,
        attribution_text: leak_str(attribution),
        attribution_url: "",
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
    /// True when the caller asked for something the stock walkers client
    /// cannot do, which is exactly when [`CustomHttpTiles`] is worth building.
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

/// Downloads tiles over HTTP with a client this process configures, so the
/// certificate policy in [`TileTlsConfig`] can be applied.
pub struct HttpTransport {
    request_tx: SyncSender<TileId>,
    tile_rx: Receiver<(TileId, Tile)>,
}

impl HttpTransport {
    /// Spawns the download workers. They are detached: dropping the transport
    /// drops `request_tx`, their `recv` fails, and each exits after finishing
    /// whatever it had in flight.
    pub fn new<S>(source: &Arc<S>, tls: &TileTlsConfig, egui_ctx: &egui::Context) -> Self
    where
        S: TileSource + Send + Sync + 'static,
    {
        // Bounded at the worker count, like walkers' request channel: a full
        // queue means "already downloading as much as we should", and the
        // rejected tile is re-requested on the next frame, by which time the
        // camera may have moved and made it irrelevant. Unbounded queueing
        // would spend the whole download budget on tiles nobody is looking at
        // any more after a fast pan.
        let (request_tx, request_rx) = std::sync::mpsc::sync_channel(MAX_PARALLEL_DOWNLOADS);
        let (tile_tx, tile_rx) = std::sync::mpsc::channel();

        match build_client(tls) {
            Ok(client) => {
                // One receiver shared by all workers: whoever is free takes the
                // next id. The lock is held only across `recv`, so the other
                // workers block on the mutex while idle and never while
                // downloading.
                let requests = Arc::new(Mutex::new(request_rx));
                for i in 0..MAX_PARALLEL_DOWNLOADS {
                    let requests = Arc::clone(&requests);
                    let source = Arc::clone(source);
                    let client = client.clone();
                    let tile_tx = tile_tx.clone();
                    let ctx = egui_ctx.clone();
                    let spawned = std::thread::Builder::new()
                        .name(format!("imzero2-tiles-{i}"))
                        .spawn(move || {
                            download_worker(&requests, &*source, &client, &tile_tx, &ctx);
                        });
                    if let Err(e) = spawned {
                        tracing::error!(error = %e, "could not spawn tile download worker");
                    }
                }
            }
            Err(e) => {
                // No workers: every request is dropped and no tile ever
                // arrives, so the map draws empty rather than falling back to
                // an unconfigured client the operator did not ask for.
                tracing::error!(error = %e, "tile HTTP client could not be built; this map will show no tiles");
            }
        }

        Self {
            request_tx,
            tile_rx,
        }
    }
}

impl TileTransport for HttpTransport {
    fn request(&mut self, tile_id: TileId) -> bool {
        match self.request_tx.try_send(tile_id) {
            Ok(()) => true,
            // Full: already downloading as much as we should. Disconnected:
            // every worker is gone, because the client failed to build or the
            // threads died. Both answer "not queued", so nothing is cached as
            // pending; neither panics inside a render loop the way walkers'
            // own send-failure path does.
            Err(TrySendError::Full(_) | TrySendError::Disconnected(_)) => false,
        }
    }

    fn poll(&mut self) -> Option<(TileId, Tile)> {
        self.tile_rx.try_recv().ok()
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

fn download_worker<S>(
    requests: &Mutex<Receiver<TileId>>,
    source: &S,
    client: &reqwest::blocking::Client,
    tile_tx: &std::sync::mpsc::Sender<(TileId, Tile)>,
    ctx: &egui::Context,
) where
    S: TileSource,
{
    // `Style::default()` rather than `Style`: walkers' Style is a unit struct
    // only while its `mvt` feature is off, and this should not break the day
    // something turns it on.
    #[expect(
        clippy::default_constructed_unit_structs,
        reason = "walkers::Style is only a unit struct without the mvt feature"
    )]
    let style = walkers::Style::default();
    loop {
        let tile_id = {
            let Ok(rx) = requests.lock() else {
                // Poisoned by a panicking sibling; this worker is done.
                return;
            };
            match rx.recv() {
                Ok(tile_id) => tile_id,
                // Transport dropped — the map is gone or its tile config
                // changed and it was rebuilt.
                Err(_) => return,
            }
        };

        let url = source.tile_url(tile_id);
        let body = client
            .get(&url)
            .send()
            .and_then(reqwest::blocking::Response::error_for_status)
            .and_then(reqwest::blocking::Response::bytes);
        let body = match body {
            Ok(body) => body,
            Err(e) => {
                tracing::warn!(url = %url, error = %e, "tile download failed");
                continue;
            }
        };

        // Decode and upload here rather than on the render thread — this is
        // the whole reason the workers exist and not just an async client.
        match Tile::new(&body, &style, tile_id.zoom, ctx) {
            Ok(tile) => {
                if tile_tx.send((tile_id, tile)).is_err() {
                    return;
                }
                // Without this a still map never repaints and the tile only
                // appears the next time the user touches something.
                ctx.request_repaint();
            }
            Err(e) => tracing::warn!(url = %url, error = %e, "tile could not be decoded"),
        }
    }
}

/// The tile source a walkers map draws from: walkers' own downloader, or ours
/// when the TLS configuration requires it.
///
/// An enum rather than a `Box<dyn Tiles>` for a borrow-checker reason, not a
/// stylistic one. `walkers::Map::new` takes `Option<&'b mut dyn Tiles>`, whose
/// elided object lifetime is `'b` — the borrow of the retained state, which is
/// not `'static`. A `&mut Box<dyn Tiles + 'static>` cannot be reborrowed to
/// that, since `&mut T` is invariant in `T`. Coercing from a concrete variant
/// can, which is what [`WalkersTiles::as_dyn`] does. It also saves the box.
pub enum WalkersTiles {
    /// walkers' downloader, for the built-in OpenStreetMap source and for any
    /// custom source that needs no TLS configuration.
    Walkers(walkers::HttpTiles),
    /// Ours, for a custom source that does.
    Custom(CustomHttpTiles),
}

impl WalkersTiles {
    pub fn as_dyn(&mut self) -> &mut dyn Tiles {
        match self {
            Self::Walkers(t) => t,
            Self::Custom(t) => t,
        }
    }
}

/// A [`Tiles`] implementation whose downloads run through a client this
/// process configures. Used in place of `walkers::HttpTiles` when — and only
/// when — [`TileTlsConfig::is_configured`] holds.
pub struct CustomHttpTiles {
    attribution: Attribution,
    tile_size: u32,
    max_zoom: u8,
    // `Some(tile)` is drawable; `None` means requested and not here yet, or
    // requested and failed. walkers does not distinguish those two either.
    cache: LruCache<TileId, Option<Tile>>,
    transport: Box<dyn TileTransport>,
}

impl CustomHttpTiles {
    pub fn new(source: CustomTileSource, tls: &TileTlsConfig, egui_ctx: &egui::Context) -> Self {
        let attribution = source.attribution();
        let tile_size = source.tile_size();
        let max_zoom = source.max_zoom();
        let source = Arc::new(source);
        let transport = Box::new(HttpTransport::new(&source, tls, egui_ctx));
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
    /// saturated transport leaves the cache untouched so the next frame asks
    /// again.
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

impl Tiles for CustomHttpTiles {
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
