//! Last-level-cache topology, for sizing advice the CPU rasterizer can give.
//!
//! Two facts turn out to govern how the software pixel host performs, and both
//! are readable at startup (measurements:
//! `doc/adr-background-work/egui-software-backend-survey.md` §16):
//!
//! 1. **Which L3 domains the worker pool may run on.** On a multi-CCD part,
//!    keeping the pool inside one domain was measured 1.33× faster at
//!    1920×1200 than letting it span two — but only while the working set fits
//!    that domain. Nothing here can pin anything: the process does not know
//!    what else the box is for. It can say so, once, and name the list.
//! 2. **How large that domain is.** The rasterizer's hot working set is the
//!    frame buffer plus the compositing canvas, `2 × w × h × 4` bytes. Past
//!    the L3 budget the cost per pixel rises and the pinning advice inverts.
//!
//! Linux only; every other target reports nothing and the host logs nothing.
//! Reading is deliberately cheap and total — anything unparseable is simply
//! absent, because a render host must not fail over a missing sysfs file.

/// What the process may run on, and the cache budget that implies.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Topology {
    /// Distinct L3 domains intersecting this process's CPU affinity. More than
    /// one means the pool will be spread by the scheduler.
    pub domains_spanned: usize,
    /// Bytes in the smallest spanned domain — the budget a pinned pool gets.
    pub l3_bytes: u64,
    /// CPU list of one spanned domain, verbatim from sysfs, so the advice can
    /// name something the operator can paste into `taskset -c`.
    pub pin_hint: String,
}

impl Topology {
    /// Frame pixels whose working set still fits `l3_bytes`. The rasterizer
    /// holds a frame buffer and a canvas, four bytes per pixel each, so the
    /// budget is `l3 / 8`. Per-primitive caches sit on top of that, which is
    /// why this is advice and not a limit: the measured crossover at 32 MB
    /// fell between a 17.5 MB working set (still winning) and a 39.5 MB one
    /// (no longer), which is what this bound picks out.
    pub fn max_pixels(&self) -> u64 {
        self.l3_bytes / 8
    }
}

/// Expand a Linux CPU range list — `"0-7,16-23"`, `"3"`, `"0-15"`. Unparseable
/// fragments are skipped rather than failing the whole list.
pub fn parse_cpu_list(s: &str) -> Vec<usize> {
    let mut out = Vec::new();
    for part in s.trim().split(',').filter(|p| !p.is_empty()) {
        match part.split_once('-') {
            Some((lo, hi)) => {
                if let (Ok(lo), Ok(hi)) = (lo.trim().parse::<usize>(), hi.trim().parse::<usize>()) {
                    if lo <= hi {
                        out.extend(lo..=hi);
                    }
                }
            }
            None => {
                if let Ok(v) = part.trim().parse::<usize>() {
                    out.push(v);
                }
            }
        }
    }
    out
}

/// Parse a sysfs cache size — `"32768K"`, `"32M"`, or a bare byte count.
pub fn parse_cache_size(s: &str) -> Option<u64> {
    let t = s.trim();
    let (digits, mult) = match t.chars().last() {
        Some('K' | 'k') => (&t[..t.len() - 1], 1024),
        Some('M' | 'm') => (&t[..t.len() - 1], 1024 * 1024),
        Some('G' | 'g') => (&t[..t.len() - 1], 1024 * 1024 * 1024),
        _ => (t, 1),
    };
    digits.trim().parse::<u64>().ok().map(|v| v * mult)
}

/// CPUs this process is allowed on, from `/proc/self/status`. Preferred over
/// `sched_getaffinity` because it needs no `libc` and no `unsafe`, which the
/// workspace denies.
#[cfg(target_os = "linux")]
fn allowed_cpus() -> Option<Vec<usize>> {
    let status = std::fs::read_to_string("/proc/self/status").ok()?;
    let line = status.lines().find_map(|l| l.strip_prefix("Cpus_allowed_list:"))?;
    let cpus = parse_cpu_list(line);
    (!cpus.is_empty()).then_some(cpus)
}

/// Every L3 domain on the machine, as `(cpu list, size in bytes)`, deduplicated
/// by the raw `shared_cpu_list` string.
#[cfg(target_os = "linux")]
fn l3_domains() -> Vec<(String, u64)> {
    let mut seen: Vec<(String, u64)> = Vec::new();
    let Ok(cpus) = std::fs::read_dir("/sys/devices/system/cpu") else {
        return seen;
    };
    for cpu in cpus.flatten() {
        let name = cpu.file_name();
        let Some(name) = name.to_str() else { continue };
        if !name.starts_with("cpu") || !name[3..].chars().all(|c| c.is_ascii_digit()) {
            continue;
        }
        let Ok(indices) = std::fs::read_dir(cpu.path().join("cache")) else {
            continue;
        };
        for idx in indices.flatten() {
            let p = idx.path();
            let level = std::fs::read_to_string(p.join("level")).ok();
            if level.as_deref().map(str::trim) != Some("3") {
                continue;
            }
            let (Ok(list), Ok(size)) = (
                std::fs::read_to_string(p.join("shared_cpu_list")),
                std::fs::read_to_string(p.join("size")),
            ) else {
                continue;
            };
            let list = list.trim().to_owned();
            let Some(bytes) = parse_cache_size(&size) else {
                continue;
            };
            if !seen.iter().any(|(l, _)| *l == list) {
                seen.push((list, bytes));
            }
        }
    }
    seen
}

/// Probe the machine. `None` when the topology cannot be read — no L3 reported
/// (containers and VMs often report none), affinity unreadable, or not Linux.
#[cfg(target_os = "linux")]
pub fn probe() -> Option<Topology> {
    let allowed = allowed_cpus()?;
    let domains = l3_domains();
    if domains.is_empty() {
        return None;
    }
    let spanned: Vec<&(String, u64)> = domains
        .iter()
        .filter(|(list, _)| parse_cpu_list(list).iter().any(|c| allowed.contains(c)))
        .collect();
    let smallest = spanned.iter().map(|(_, b)| *b).min()?;
    // Hint with the domain holding the most allowed CPUs — the one a pin would
    // most plausibly target.
    let hint = spanned
        .iter()
        .max_by_key(|(list, _)| parse_cpu_list(list).iter().filter(|c| allowed.contains(c)).count())
        .map(|(list, _)| list.clone())?;
    Some(Topology {
        domains_spanned: spanned.len(),
        l3_bytes: smallest,
        pin_hint: hint,
    })
}

#[cfg(not(target_os = "linux"))]
pub fn probe() -> Option<Topology> {
    None
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn cpu_lists_parse_the_shapes_sysfs_emits() {
        assert_eq!(parse_cpu_list("0-3"), vec![0, 1, 2, 3]);
        assert_eq!(parse_cpu_list("5"), vec![5]);
        assert_eq!(parse_cpu_list("0-1,4-5"), vec![0, 1, 4, 5]);
        assert_eq!(parse_cpu_list(" 0-2 \n"), vec![0, 1, 2]);
        assert!(parse_cpu_list("").is_empty());
        // Garbage is skipped, not fatal, and does not take the list with it.
        assert_eq!(parse_cpu_list("nonsense,7"), vec![7]);
        assert!(
            parse_cpu_list("9-3").is_empty(),
            "reversed ranges yield nothing"
        );
    }

    #[test]
    fn cache_sizes_parse_with_and_without_a_suffix() {
        assert_eq!(parse_cache_size("32768K"), Some(32 * 1024 * 1024));
        assert_eq!(parse_cache_size("32M"), Some(32 * 1024 * 1024));
        assert_eq!(parse_cache_size("1G"), Some(1024 * 1024 * 1024));
        assert_eq!(parse_cache_size(" 512 "), Some(512));
        assert_eq!(parse_cache_size("bogus"), None);
    }

    #[test]
    fn max_pixels_is_the_frame_plus_canvas_budget() {
        let t = Topology {
            domains_spanned: 1,
            l3_bytes: 32 * 1024 * 1024,
            pin_hint: "0-15".to_owned(),
        };
        // 32 MB / 8 bytes per pixel = 4.19 Mpx, so 1920x1200 (2.30 Mpx) fits
        // and 2880x1800 (5.18 Mpx) does not — the measured crossover.
        assert_eq!(t.max_pixels(), 4 * 1024 * 1024);
        assert!(u64::from(1920u32 * 1200) < t.max_pixels());
        assert!(u64::from(2880u32 * 1800) > t.max_pixels());
    }
}
