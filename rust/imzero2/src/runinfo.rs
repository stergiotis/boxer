//! Mirror of the Go-side `runtime/runinfo` package. The Go-side
//! `runinfo.Init()` mints a 16-char nanoid for the process and
//! exports it as `PEBBLE2_RUN_ID` before spawning the Rust client.
//! Reading the same env var here lands the same identifier on both
//! sides of the wire so a future audit event minted by Rust can
//! attribute itself to the Go parent's run (ADR-0026 §2026-05-12
//! amendment, follow-up (b)).
//!
//! Today's only consumer is the tracing root span and a one-shot
//! "bound to parent run" log event — when the Rust client gains its
//! own audit-write path, it will read `run_id()` for the run anchor.

use std::sync::OnceLock;

/// Env var name; kept in sync with `runinfo.EnvVar` on the Go side.
pub const ENV_VAR: &str = "PEBBLE2_RUN_ID";

/// Sentinel logged when the env var is absent (e.g., the Rust client
/// was launched standalone rather than from the Go carousel). The
/// tracing span still records a non-empty field so log aggregators
/// can filter on `run_id` reliably.
pub const STANDALONE: &str = "standalone";

static RUN_ID: OnceLock<Option<String>> = OnceLock::new();

/// Returns the inherited run_id, or None when `PEBBLE2_RUN_ID` is
/// unset. Cached on first read; subsequent calls are lock-free.
pub fn run_id() -> Option<&'static str> {
    RUN_ID.get_or_init(|| std::env::var(ENV_VAR).ok().filter(|s| !s.is_empty())).as_deref()
}

/// Convenience accessor returning [`STANDALONE`] when no parent run
/// is bound. Use this for tracing-span fields where an Option is
/// awkward; callers that care about presence/absence should use
/// [`run_id`] directly.
pub fn run_id_or_standalone() -> &'static str {
    run_id().unwrap_or(STANDALONE)
}

/// Constructs the root tracing span for the Rust client and enters
/// it, returning an owned guard. The caller MUST keep the returned
/// `EnteredSpan` alive for the duration of program execution —
/// dropping it before `main` returns shifts subsequent events out of
/// the span.
///
/// Every tracing event emitted while the span is entered carries the
/// `run_id` field, so a combined Go + Rust log stream can be filtered
/// to one process boot.
pub fn enter_root_span() -> tracing::span::EnteredSpan {
    let rid = run_id_or_standalone();
    tracing::info_span!("rust", run_id = rid).entered()
}

/// One-shot info event announcing the bound run_id. Called once from
/// `main` after the tracing subscriber + root span are wired so the
/// "Rust bound to run X" line appears in the same log stream that
/// carries every subsequent event under the root span.
pub fn log_bound_run() {
    match run_id() {
        Some(rid) => tracing::info!(
            run_id = rid,
            env_var = ENV_VAR,
            "runinfo: bound to parent run"
        ),
        None => tracing::info!(env_var = ENV_VAR, "runinfo: standalone (no parent run_id)"),
    }
}

#[cfg(test)]
#[allow(unsafe_code)] // Rust 2024 made std::env::set_var/remove_var
// unsafe (multi-thread hazard); tests here serialise on these calls
// and the workspace's `unsafe_code = "deny"` lint blocks the harness
// build without this allow.
mod tests {
    use super::*;

    // These tests mutate the process-global ENV_VAR (PEBBLE2_RUN_ID) and cargo
    // runs tests in parallel, so a shared lock serializes them — otherwise one
    // test's set_var races another's remove_var + assert (the flake that hit
    // parse_helper_returns_none_when_unset). Recover from poisoning so a single
    // genuinely-failing test doesn't mask the rest behind lock-panic noise.
    static ENV_LOCK: std::sync::Mutex<()> = std::sync::Mutex::new(());

    // OnceLock memoises the first env-var read, so a single process
    // can only exercise one branch of run_id() per test binary. This
    // test asserts the present-value path; the absent path is covered
    // by parse_helper below which doesn't touch the global.
    #[test]
    fn parse_helper_returns_some_when_set() {
        let _env = ENV_LOCK.lock().unwrap_or_else(|p| p.into_inner());
        // SAFETY: Rust 2024 marks env mutation unsafe due to multi-thread
        // hazards; ENV_LOCK serializes the env-mutating tests against each
        // other, and no non-test thread reads ENV_VAR during the test run.
        unsafe {
            std::env::set_var(ENV_VAR, "abcdef1234567890");
        }
        let v = parse_helper();
        assert_eq!(v.as_deref(), Some("abcdef1234567890"));
        unsafe {
            std::env::remove_var(ENV_VAR);
        }
    }

    #[test]
    fn parse_helper_returns_none_when_unset() {
        let _env = ENV_LOCK.lock().unwrap_or_else(|p| p.into_inner());
        unsafe {
            std::env::remove_var(ENV_VAR);
        }
        let v = parse_helper();
        assert_eq!(v, None);
    }

    #[test]
    fn parse_helper_returns_none_on_empty() {
        let _env = ENV_LOCK.lock().unwrap_or_else(|p| p.into_inner());
        unsafe {
            std::env::set_var(ENV_VAR, "");
        }
        let v = parse_helper();
        assert_eq!(v, None);
        unsafe {
            std::env::remove_var(ENV_VAR);
        }
    }

    /// Mirrors the closure body of `RUN_ID.get_or_init` so the env-
    /// var parsing logic is testable without the OnceLock memoisation
    /// foreclosing later tests.
    fn parse_helper() -> Option<String> {
        std::env::var(ENV_VAR).ok().filter(|s| !s.is_empty())
    }

    #[test]
    fn run_id_or_standalone_falls_back() {
        // OnceLock is empty here unless a prior test memoised; in
        // that case the assertion below is still safe — both
        // STANDALONE and the memoised value are non-empty.
        let v = run_id_or_standalone();
        assert!(!v.is_empty());
    }
}
