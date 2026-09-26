//! Native peer and pass-through counter.
//!
//! `fffi2stub <table-file> [stage-w stage-h]` — the stub peer: reads the FFFI2
//! stream on stdin, writes replies on stdout, prints its counters on stderr at
//! EOF.
//!
//! `fffi2stub tee [--opcodes FILE] [--frame-op N] [--report FILE] -- CLIENT
//! ARGS...` — the counting pass-through: launches the real client, forwards
//! stdin to it and its stdout back, and counts what Go sends per frame and per
//! opcode. Nothing is answered or altered. The per-frame table goes to
//! `--report` (TSV), the per-opcode summary to stderr at exit. Frames end at
//! `--frame-op`: `FetchFrameMetrics` in the egui2 bindings, resolved by name
//! from the `--opcodes` table (the `wasmspike -dumpOpcodes` output) when one
//! is given, since the number moves whenever the IDL gains an opcode; 38
//! without a table.

use std::io::{Read, Write};

fn main() {
    let args: Vec<String> = std::env::args().collect();
    if args.get(1).map(String::as_str) == Some("tee") {
        tee(&args[2..]);
        return;
    }
    let table = std::fs::read_to_string(
        args.get(1)
            .expect("usage: fffi2stub <table-file> [w h] | fffi2stub tee ..."),
    )
    .expect("read table");
    let mut stub = fffi2stub::Stub::new();
    stub.load_table(&table).expect("fetch table");
    if args.len() >= 4 {
        stub.set_stage(args[2].parse().unwrap(), args[3].parse().unwrap());
    }
    let mut stdin = std::io::stdin().lock();
    let mut stdout = std::io::stdout().lock();
    let mut buf = vec![0u8; 1 << 16];
    loop {
        let n = match stdin.read(&mut buf) {
            Ok(0) => break,
            Ok(n) => n,
            Err(e) if e.kind() == std::io::ErrorKind::Interrupted => continue,
            Err(_) => break,
        };
        stub.consume(&buf[..n]);
        let reply = stub.take_reply();
        if !reply.is_empty() {
            if stdout.write_all(&reply).is_err() {
                break;
            }
            if stdout.flush().is_err() {
                break;
            }
        }
    }
    let s = stub.stats;
    eprintln!(
        "fffi2stub: messages={} bytes={} frames={} fetches={} max_message={}",
        s.messages, s.bytes, s.frames, s.fetches, s.max_message
    );
}

fn tee(args: &[String]) {
    let mut opcodes: Option<String> = None;
    let mut frame_op: Option<u32> = None;
    let mut report: Option<String> = None;
    let mut i = 0;
    while i < args.len() {
        match args[i].as_str() {
            "--opcodes" => {
                opcodes = Some(args[i + 1].clone());
                i += 2;
            }
            "--frame-op" => {
                frame_op = Some(args[i + 1].parse().expect("--frame-op N"));
                i += 2;
            }
            "--report" => {
                report = Some(args[i + 1].clone());
                i += 2;
            }
            "--" => {
                i += 1;
                break;
            }
            other => panic!("fffi2stub tee: unknown option {other}"),
        }
    }
    let client = &args[i..];
    assert!(!client.is_empty(), "fffi2stub tee: no client after --");
    let mut names: Vec<String> = vec![String::new(); 4096];
    if let Some(p) = &opcodes {
        for line in std::fs::read_to_string(p).expect("read opcodes").lines() {
            let mut it = line.split_whitespace();
            if let (Some(id), Some(name)) = (it.next(), it.next())
                && let Ok(id) = id.parse::<usize>()
                && id < names.len()
            {
                names[id] = name.to_string();
            }
        }
    }
    let mut child = std::process::Command::new(&client[0])
        .args(&client[1..])
        .stdin(std::process::Stdio::piped())
        .stdout(std::process::Stdio::piped())
        .spawn()
        .expect("spawn client");
    let mut child_in = child.stdin.take().unwrap();
    let mut child_out = child.stdout.take().unwrap();

    // replies: client stdout -> our stdout, unchanged and unbuffered
    let down = std::thread::spawn(move || {
        let mut out = std::io::stdout().lock();
        let mut buf = vec![0u8; 1 << 16];
        let mut total = 0u64;
        loop {
            let n = match child_out.read(&mut buf) {
                Ok(0) => break,
                Ok(n) => n,
                Err(e) if e.kind() == std::io::ErrorKind::Interrupted => continue,
                Err(_) => break,
            };
            total += n as u64;
            if out.write_all(&buf[..n]).is_err() || out.flush().is_err() {
                break;
            }
        }
        total
    });

    // commands: our stdin -> client stdin, counted on the way. The report
    // grows a line per closed frame, so a host torn down mid-run still
    // leaves every completed frame on disk.
    let mut report_file = report.as_ref().map(|p| {
        eprintln!("fffi2stub tee: report {p}");
        let mut f = std::fs::File::create(p).expect("create report");
        writeln!(f, "frame\tmessages\tbytes\tmax_message").unwrap();
        f
    });
    let mut reported = 0usize;
    let frame_op = frame_op.unwrap_or_else(|| {
        names
            .iter()
            .position(|n| n == "FetchFrameMetrics")
            .map(|i| i as u32)
            .unwrap_or(38)
    });
    let mut counter = fffi2stub::Counter::new(frame_op);
    let mut stdin = std::io::stdin().lock();
    let mut buf = vec![0u8; 1 << 16];
    loop {
        let n = match stdin.read(&mut buf) {
            Ok(0) => break,
            Ok(n) => n,
            Err(e) if e.kind() == std::io::ErrorKind::Interrupted => continue,
            Err(_) => break,
        };
        counter.consume(&buf[..n]);
        if child_in.write_all(&buf[..n]).is_err() || child_in.flush().is_err() {
            break;
        }
        if let Some(f) = report_file.as_mut() {
            let before = reported;
            while reported < counter.frames.len() {
                let (m, b, x) = counter.frames[reported];
                writeln!(f, "{reported}\t{m}\t{b}\t{x}").unwrap();
                reported += 1;
            }
            let _ = f.flush();
            // the per-opcode table, rewritten every 30 frames for the same reason
            if reported / 30 != before / 30 {
                write_opcodes(report.as_deref().unwrap(), &counter, &names);
            }
        }
    }
    drop(child_in);
    let reply_bytes = down.join().unwrap_or(0);
    let _ = child.wait();

    let frames = &counter.frames;
    let q = |sel: fn(&(u64, u64, u64)) -> u64| -> (u64, u64, u64) {
        let mut v: Vec<u64> = frames.iter().map(sel).collect();
        v.sort_unstable();
        if v.is_empty() {
            return (0, 0, 0);
        }
        (
            v[v.len() / 2],
            v[(v.len() * 9 / 10).min(v.len() - 1)],
            v[v.len() - 1],
        )
    };
    let (m50, m90, mmax) = q(|f| f.0);
    let (b50, b90, bmax) = q(|f| f.1);
    eprintln!(
        "fffi2stub tee: frames={} messages/frame p50={} p90={} max={} bytes/frame p50={} p90={} max={} reply_bytes={}",
        frames.len(),
        m50,
        m90,
        mmax,
        b50,
        b90,
        bmax,
        reply_bytes
    );
    let mut ops: Vec<(usize, u64, u64)> = counter
        .by_op
        .iter()
        .enumerate()
        .filter(|(_, e)| e.0 > 0)
        .map(|(op, e)| (op, e.0, e.1))
        .collect();
    let nf = frames.len().max(1) as u64;
    ops.sort_by_key(|e| std::cmp::Reverse(e.1));
    eprintln!("fffi2stub tee: top opcodes by messages per frame (opcode name messages bytes):");
    for (op, m, b) in ops.iter().take(25) {
        eprintln!("  {op:4} {:<32} {:>8} {:>10}", names[*op], m / nf, b / nf);
    }
    ops.sort_by_key(|e| std::cmp::Reverse(e.2));
    eprintln!("fffi2stub tee: top opcodes by bytes per frame:");
    for (op, m, b) in ops.iter().take(10) {
        eprintln!("  {op:4} {:<32} {:>8} {:>10}", names[*op], m / nf, b / nf);
    }
}

/// Writes `<report>.opcodes.tsv`: per opcode, messages and bytes per frame so far.
fn write_opcodes(report: &str, counter: &fffi2stub::Counter, names: &[String]) {
    let nf = counter.frames.len().max(1) as u64;
    let mut ops: Vec<(usize, u64, u64)> = counter
        .by_op
        .iter()
        .enumerate()
        .filter(|(_, e)| e.0 > 0)
        .map(|(op, e)| (op, e.0, e.1))
        .collect();
    ops.sort_by_key(|e| std::cmp::Reverse(e.2));
    if let Ok(mut f) = std::fs::File::create(format!("{report}.opcodes.tsv")) {
        let _ = writeln!(
            f,
            "opcode\tname\tmessages_per_frame\tbytes_per_frame\tframes"
        );
        for (op, m, b) in ops {
            let _ = writeln!(
                f,
                "{op}\t{}\t{:.2}\t{:.0}\t{nf}",
                names[op],
                m as f64 / nf as f64,
                b as f64 / nf as f64
            );
        }
    }
}
