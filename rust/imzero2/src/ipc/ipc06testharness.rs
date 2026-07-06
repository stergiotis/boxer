use crate::ipc::ipc06::MeshNode;
use rand::Rng;
use std::io::{self, Write};
use std::thread;
use std::time::Duration;

pub fn run_producer(shm_path: &str, data_size: usize) {
    let mut node = MeshNode::new(shm_path, "producer").unwrap();
    let mut hasher = blake3::Hasher::new();
    let mut rng = rand::rng();
    let mut written_total = 0;
    let max_chunk = 64 * 1024;

    while written_total < data_size {
        // Generate random chunk size
        let chunk_len = rng.random_range(1..=max_chunk).min(data_size - written_total);
        let mut chunk = vec![0u8; chunk_len];
        rng.fill(&mut chunk[..]);

        // Hash and Write
        hasher.update(&chunk);
        node.write(&chunk);

        written_total += chunk_len;
        tracing::trace!(
            size = chunk.len(),
            total = written_total,
            "ipc producer: sent data"
        );

        // Minor yield to allow consumer interaction
        if written_total % (1024 * 1024) == 0 {
            thread::sleep(Duration::from_micros(100));
        }
    }

    // Print Hash to Stdout for the Go Test Runner to capture
    print!("{}", hasher.finalize().to_hex());
    io::stdout().flush().unwrap();
}

pub fn run_consumer(shm_path: &str, data_size: usize) {
    let mut node = MeshNode::new(shm_path, "consumer").unwrap();
    let mut hasher = blake3::Hasher::new();
    let mut read_total = 0;

    node.seek_to_start();

    while read_total < data_size {
        match node.read() {
            Ok(Some(data)) => {
                tracing::trace!(
                    size = data.len(),
                    total = read_total,
                    "ipc consumer: received data"
                );
                hasher.update(&data);
                read_total += data.len();
            }
            Ok(None) => {
                // Busy wait / spin
                thread::sleep(Duration::from_micros(10));
            }
            Err(_) => {
                // In integrity tests, we fail on gap jumps
                std::process::exit(1);
            }
        }
    }

    print!("{}", hasher.finalize().to_hex());
    io::stdout().flush().unwrap();
}
