use memmap2::MmapMut;
use std::fs::OpenOptions;
use std::os::unix::fs::OpenOptionsExt;
use std::sync::atomic::{AtomicI64, AtomicU32, AtomicU64, Ordering};
use std::time::{SystemTime, UNIX_EPOCH};
use std::ptr;

const BUFFER_SIZE: u64 = 1024 * 1024 * 64;
const MSG_HEADER_SIZE: u64 = 8;
const MAX_CONSUMERS: usize = 16;

const MAGIC_CONSTANT: u32 = 0xcafebabe;
const PROTOCOL_VER: u32 = 0;

// ---------------------------------------------------------
// Layout
// ---------------------------------------------------------

#[repr(C)]
struct FileHeader {
    magic: u32,
    version: u32,
}

#[repr(C)]
struct ConsumerSlot {
    read_pos: AtomicU64,
    heartbeat: AtomicI64,
    active: AtomicU32,
    _pad: [u8; 44],
}

#[repr(C)]
struct Metadata {
    write_cursor: AtomicU64,
    _pad: [u8; 56],
    consumers: [ConsumerSlot; MAX_CONSUMERS],
}

// Helper for offsets
const FILE_HEADER_SIZE: usize = std::mem::size_of::<FileHeader>();
const METADATA_SIZE: usize = std::mem::size_of::<Metadata>();

pub struct MeshNode {
    _mmap: MmapMut,
    header: *mut FileHeader,
    meta: *const Metadata,
    data_buffer: *mut u8,
    local_pos: u64,
    slot_idx: Option<usize>,
}

#[allow(unsafe_code)]
unsafe impl Send for MeshNode {}
#[allow(unsafe_code)]
unsafe impl Sync for MeshNode {}

impl MeshNode {
    pub fn new(shm_path: &str, mode: &str) -> Result<Self, Box<dyn std::error::Error>> {
        let file = OpenOptions::new()
            .read(true)
            .write(true)
            .create(mode == "producer")
            .mode(0o600)
            .open(shm_path)?;

        let total_size = (FILE_HEADER_SIZE + METADATA_SIZE) as u64 + BUFFER_SIZE;

        if mode == "producer" {
            file.set_len(total_size)?;
        }

#[allow(unsafe_code)]
        let mut mmap = unsafe { MmapMut::map_mut(&file)? };

        // Pointer Arithmetic
        let base_ptr = mmap.as_mut_ptr();

        let header_ptr = base_ptr as *mut FileHeader;

        // Metadata starts after FileHeader
#[allow(unsafe_code)]
        let meta_ptr = unsafe { base_ptr.add(FILE_HEADER_SIZE) } as *const Metadata;

        // Buffer starts after FileHeader + Metadata
#[allow(unsafe_code)]
        let buffer_ptr = unsafe { base_ptr.add(FILE_HEADER_SIZE + METADATA_SIZE) };

        let mut node = MeshNode {
            _mmap: mmap,
            header: header_ptr,
            meta: meta_ptr,
            data_buffer: buffer_ptr,
            local_pos: 0,
            slot_idx: None,
        };

        if mode == "producer" {
            // Write Magic & Version
#[allow(unsafe_code)]
            unsafe {
                (*node.header).magic = MAGIC_CONSTANT;
                (*node.header).version = PROTOCOL_VER;
            }
        } else {
            // Validate Magic & Version
#[allow(unsafe_code)]
            unsafe {
                if (*node.header).magic != MAGIC_CONSTANT {
                    return Err(format!("Invalid Magic: {:X}", (*node.header).magic).into());
                }
                if (*node.header).version != PROTOCOL_VER {
                    return Err(format!("Invalid Version: {}", (*node.header).version).into());
                }
                node.local_pos = (*node.meta).write_cursor.load(Ordering::Relaxed);
            }
            node.register_consumer()?;
        }

        Ok(node)
    }

    fn register_consumer(&mut self) -> Result<(), std::io::Error> {
#[allow(unsafe_code)]
        let meta = unsafe { &*self.meta };
        for (i, slot) in meta.consumers.iter().enumerate() {
            if slot.active.compare_exchange(0, 1, Ordering::SeqCst, Ordering::Relaxed).is_ok() {
                self.slot_idx = Some(i);
                let now = SystemTime::now().duration_since(UNIX_EPOCH).unwrap().as_nanos() as i64;
                slot.heartbeat.store(now, Ordering::Relaxed);
                slot.read_pos.store(self.local_pos, Ordering::Relaxed);
                tracing::trace!("registered rust consumer #{}", i);
                return Ok(());
            }
        }
        Err(std::io::Error::new(std::io::ErrorKind::Other, "No consumer slots available"))
    }

    fn unregister(&self) {
        if let Some(idx) = self.slot_idx {
#[allow(unsafe_code)]
            let meta = unsafe { &*self.meta };
            meta.consumers[idx].active.store(0, Ordering::SeqCst);
        }
    }
    pub fn seek_to_start(&mut self) {
        self.local_pos = 0;

        // Update the shared memory slot so the producer monitoring
        // sees that we are actually at 0 (and not lagging by billions of bytes)
        if let Some(idx) = self.slot_idx {
            #[allow(unsafe_code)]
            let meta = unsafe { &*self.meta };
            meta.consumers[idx]
                .read_pos
                .store(0, Ordering::Relaxed);
        }
    }

    // Producer Write
    pub fn write(&mut self, payload: &[u8]) {
#[allow(unsafe_code)]
        let meta = unsafe { &*self.meta };
        let msg_len = payload.len() as u32;

        let current_pos = meta.write_cursor.load(Ordering::Relaxed);
        let (aligned_pos, ring_offset) = self.align_cursor(current_pos);
        let current_lap = (aligned_pos / BUFFER_SIZE) as u32;

        // Write Payload
        let payload_start = (ring_offset + MSG_HEADER_SIZE) % BUFFER_SIZE;
        self.write_raw_bytes(payload_start, payload);

        // Write Header
        let mut header = [0u8; 8];
        header[0..4].copy_from_slice(&current_lap.to_le_bytes());
        header[4..8].copy_from_slice(&msg_len.to_le_bytes());

        self.write_raw_bytes(ring_offset, &header);

        let next_pos = aligned_pos + MSG_HEADER_SIZE + msg_len as u64;
        meta.write_cursor.store(next_pos, Ordering::Release);
    }

    // Consumer Read
    pub fn read(&mut self) -> Result<Option<Vec<u8>>, String> {
        let Some(slot_idx) = self.slot_idx else {
            return Err("read called on unregistered node (producer-mode or registration failed)".to_string());
        };
#[allow(unsafe_code)]
        let meta = unsafe { &*self.meta };
        let slot = &meta.consumers[slot_idx];

        // Heartbeat
        let now = SystemTime::now().duration_since(UNIX_EPOCH).unwrap().as_nanos() as i64;
        slot.heartbeat.store(now, Ordering::Relaxed);
        slot.read_pos.store(self.local_pos, Ordering::Release);

        let remote_pos = meta.write_cursor.load(Ordering::Acquire);

        if self.local_pos >= remote_pos {
            return Ok(None);
        }

        let (aligned_pos, ring_offset) = self.align_cursor(self.local_pos);

        if remote_pos - aligned_pos > BUFFER_SIZE {
            self.local_pos = remote_pos;
            return Err("Gap jump".to_string());
        }

        // Seqlock Check
        let header_bytes = self.read_raw_bytes(ring_offset, 8);
        let pre_lap = u32::from_le_bytes(header_bytes[0..4].try_into().unwrap());
        let msg_len = u32::from_le_bytes(header_bytes[4..8].try_into().unwrap());

        if msg_len as u64 > BUFFER_SIZE { return Ok(None); }

        let payload_start = (ring_offset + MSG_HEADER_SIZE) % BUFFER_SIZE;
        let payload = self.read_raw_bytes(payload_start, msg_len as u64);

        // Atomic Load for Post-Check
#[allow(unsafe_code)]
        let post_lap = unsafe {
            // We align logic guarantees header is contiguous (align_cursor moves us if <8 bytes remain)
            let ptr = self.data_buffer.add(ring_offset as usize) as *const AtomicU32;
            (*ptr).load(Ordering::Acquire)
        };
        let post_lap = u32::from_le(post_lap);

        let expected_lap = (aligned_pos / BUFFER_SIZE) as u32;

        if pre_lap != expected_lap || post_lap != expected_lap {
            self.local_pos = remote_pos;
            return Err("Tearing".to_string());
        }

        self.local_pos = aligned_pos + MSG_HEADER_SIZE + msg_len as u64;
        Ok(Some(payload))
    }

    fn align_cursor(&self, mut cursor: u64) -> (u64, u64) {
        let mut ring_offset = cursor % BUFFER_SIZE;
        let remaining = BUFFER_SIZE - ring_offset;
        if remaining < MSG_HEADER_SIZE {
            cursor += remaining;
            ring_offset = 0;
        }
        (cursor, ring_offset)
    }

    fn write_raw_bytes(&self, offset: u64, data: &[u8]) {
        let offset = offset as usize;
        let size = data.len();
#[allow(unsafe_code)]
        unsafe {
            if (offset + size) <= BUFFER_SIZE as usize {
                ptr::copy_nonoverlapping(data.as_ptr(), self.data_buffer.add(offset), size);
            } else {
                let first_chunk = (BUFFER_SIZE as usize) - offset;
                ptr::copy_nonoverlapping(data.as_ptr(), self.data_buffer.add(offset), first_chunk);
                ptr::copy_nonoverlapping(data.as_ptr().add(first_chunk), self.data_buffer, size - first_chunk);
            }
        }
    }

    fn read_raw_bytes(&self, offset: u64, size: u64) -> Vec<u8> {
        let offset = offset as usize;
        let size = size as usize;
        let mut out = vec![0u8; size];
#[allow(unsafe_code)]
        unsafe {
            if (offset + size) <= BUFFER_SIZE as usize {
                ptr::copy_nonoverlapping(self.data_buffer.add(offset), out.as_mut_ptr(), size);
            } else {
                let first_chunk = (BUFFER_SIZE as usize) - offset;
                ptr::copy_nonoverlapping(self.data_buffer.add(offset), out.as_mut_ptr(), first_chunk);
                ptr::copy_nonoverlapping(self.data_buffer, out.as_mut_ptr().add(first_chunk), size - first_chunk);
            }
        }
        out
    }
}
