//! The host's monotonic clock. Natively it is `std::time::Instant`; on
//! wasm32 `Instant::now` panics, so the browser host reads milliseconds from
//! an import the JS side provides (`env.now_ms`, `performance.now`). Every
//! per-frame timing in the shared interpreter goes through this type so the
//! same code measures itself in both places.

#[cfg(not(target_arch = "wasm32"))]
pub use std::time::Instant;

#[cfg(target_arch = "wasm32")]
pub use wasm::Instant;

#[cfg(target_arch = "wasm32")]
mod wasm {
    use std::time::Duration;

    #[allow(unsafe_code)]
    mod imports {
        #[link(wasm_import_module = "env")]
        unsafe extern "C" {
            pub fn now_ms() -> f64;
        }
    }

    /// A point in time read from the host, in milliseconds since an
    /// arbitrary origin.
    #[derive(Clone, Copy, Debug, PartialEq, PartialOrd)]
    pub struct Instant(f64);

    impl Instant {
        pub fn now() -> Self {
            // SAFETY: the import takes no arguments and returns a plain f64; the
            // host binds it to `performance.now` before instantiation.
            #[allow(unsafe_code)]
            Self(unsafe { imports::now_ms() })
        }
        pub fn elapsed(&self) -> Duration {
            Self::now() - *self
        }
        pub fn duration_since(&self, earlier: Self) -> Duration {
            *self - earlier
        }
    }

    impl std::ops::Sub for Instant {
        type Output = Duration;
        fn sub(self, rhs: Self) -> Duration {
            Duration::from_secs_f64(((self.0 - rhs.0) / 1000.0).max(0.0))
        }
    }

    impl std::ops::Add<Duration> for Instant {
        type Output = Self;
        fn add(self, rhs: Duration) -> Self {
            Self(self.0 + rhs.as_secs_f64() * 1000.0)
        }
    }
}
