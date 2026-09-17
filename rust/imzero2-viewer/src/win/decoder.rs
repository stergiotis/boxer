//! FFmpeg ownership and D3D11VA selection (ADR-0243 §SD3/SD4).
use crate::wire::{pb, Codec};
use anyhow::{bail, ensure, Result};
use ffmpeg_sys_next as av;
use std::{
    ffi::{CStr, CString},
    ptr,
};
use windows::{core::Interface, Win32::Graphics::Direct3D11::ID3D11Device};

#[allow(
    non_camel_case_types,
    non_snake_case,
    dead_code,
    clippy::upper_case_acronyms
)]
mod hw {
    type ID3D11Device = std::ffi::c_void;
    type ID3D11DeviceContext = std::ffi::c_void;
    type ID3D11VideoDevice = std::ffi::c_void;
    type ID3D11VideoContext = std::ffi::c_void;
    type UINT = u32;
    include!(concat!(env!("OUT_DIR"), "/d3d11_context.rs"));
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum DecodeMode {
    Auto,
    Software,
    Hardware,
}

pub struct Frame(pub *mut av::AVFrame);
impl Drop for Frame {
    fn drop(&mut self) {
        unsafe { av::av_frame_free(&mut self.0) }
    }
}
impl Frame {
    fn new() -> Result<Self> {
        let frame = unsafe { av::av_frame_alloc() };
        ensure!(!frame.is_null(), "allocate decoded frame");
        Ok(Self(frame))
    }
}

pub struct Decoder {
    context: *mut av::AVCodecContext,
    packet: *mut av::AVPacket,
    hardware: bool,
    waiting: bool,
    codec: Codec,
    mode: DecodeMode,
    device: ID3D11Device,
    pub label: String,
}
impl Drop for Decoder {
    fn drop(&mut self) {
        unsafe {
            av::av_packet_free(&mut self.packet);
            av::avcodec_free_context(&mut self.context);
        }
    }
}

pub fn check(code: i32, what: &str) -> Result<()> {
    if code >= 0 {
        return Ok(());
    }
    let mut text = [0i8; 256];
    unsafe {
        av::av_strerror(code, text.as_mut_ptr(), text.len());
    }
    let reason = unsafe { CStr::from_ptr(text.as_ptr()) }.to_string_lossy();
    bail!("{what}: {reason}")
}

unsafe extern "C" fn hardware_format(
    _ctx: *mut av::AVCodecContext,
    formats: *const av::AVPixelFormat,
) -> av::AVPixelFormat {
    unsafe {
        let mut p = formats;
        while *p != av::AVPixelFormat::AV_PIX_FMT_NONE {
            if *p == av::AVPixelFormat::AV_PIX_FMT_D3D11 {
                return *p;
            }
            p = p.add(1);
        }
    }
    av::AVPixelFormat::AV_PIX_FMT_NONE
}

impl Decoder {
    pub fn new(device: &ID3D11Device, codec: Codec, mode: DecodeMode) -> Result<Self> {
        match Self::open(device, codec, mode, mode != DecodeMode::Software) {
            Ok(d) => Ok(d),
            Err(e) if mode == DecodeMode::Auto => {
                let mut d = Self::open(device, codec, mode, false)?;
                d.label = format!("software decode (hardware unavailable: {e})");
                Ok(d)
            }
            Err(e) => Err(e),
        }
    }
    fn open(device: &ID3D11Device, codec: Codec, mode: DecodeMode, hardware: bool) -> Result<Self> {
        unsafe {
            let id = match codec {
                Codec::H264 => av::AVCodecID::AV_CODEC_ID_H264,
                Codec::Vp9 => av::AVCodecID::AV_CODEC_ID_VP9,
                Codec::Av1 => av::AVCodecID::AV_CODEC_ID_AV1,
            };
            let implementation = if codec == Codec::Av1 {
                av::avcodec_find_decoder_by_name(
                    CString::new(if hardware { "av1" } else { "libdav1d" })?.as_ptr(),
                )
            } else {
                av::avcodec_find_decoder(id)
            };
            ensure!(
                !implementation.is_null(),
                "required decoder not included in this FFmpeg build"
            );
            let context = av::avcodec_alloc_context3(implementation);
            ensure!(!context.is_null(), "allocate decoder context");
            let mut d = Self {
                context,
                packet: ptr::null_mut(),
                hardware,
                waiting: true,
                codec,
                mode,
                device: device.clone(),
                label: if hardware {
                    "D3D11VA pending first frame".into()
                } else {
                    "software decode".into()
                },
            };
            d.packet = av::av_packet_alloc();
            ensure!(!d.packet.is_null(), "allocate decoder packet");
            (*context).thread_count = 2;
            (*context).thread_type = av::FF_THREAD_SLICE;
            (*context).pkt_timebase = av::AVRational {
                num: 1,
                den: 1_000_000,
            };
            (*context).max_pixels = 8192 * 8192;
            if hardware {
                let mut found = false;
                for i in 0..64 {
                    let config = av::avcodec_get_hw_config(implementation, i);
                    if config.is_null() {
                        break;
                    }
                    if (*config).device_type == av::AVHWDeviceType::AV_HWDEVICE_TYPE_D3D11VA
                        && (*config).pix_fmt == av::AVPixelFormat::AV_PIX_FMT_D3D11
                    {
                        found = true;
                        break;
                    }
                }
                ensure!(found, "decoder has no D3D11VA configuration");
                let mut reference =
                    av::av_hwdevice_ctx_alloc(av::AVHWDeviceType::AV_HWDEVICE_TYPE_D3D11VA);
                ensure!(!reference.is_null(), "allocate D3D11 hardware context");
                let hardware_context = (*reference).data as *mut av::AVHWDeviceContext;
                let native = (*hardware_context).hwctx as *mut hw::AVD3D11VADeviceContext;
                (*native).device = device.clone().into_raw().cast();
                let result = av::av_hwdevice_ctx_init(reference);
                if result < 0 {
                    av::av_buffer_unref(&mut reference);
                    check(result, "initialize D3D11VA")?;
                }
                (*context).hw_device_ctx = reference;
                (*context).get_format = Some(hardware_format);
            }
            check(
                av::avcodec_open2(context, implementation, ptr::null_mut()),
                "open decoder",
            )?;
            Ok(d)
        }
    }

    pub fn push(&mut self, chunk: &pb::VideoChunk) -> Result<Vec<Frame>> {
        if self.waiting && !chunk.keyframe {
            return Ok(Vec::new());
        }
        match self.decode(chunk) {
            Ok(frames) => Ok(frames),
            Err(error) if self.hardware && self.mode == DecodeMode::Auto => {
                // Only replay an independently decodable AU. Otherwise reset the connection.
                let device = self.device.clone();
                let mut fallback = Self::open(&device, self.codec, self.mode, false)?;
                fallback.label = format!("software decode (D3D11VA failed: {error})");
                *self = fallback;
                ensure!(
                    chunk.keyframe,
                    "hardware decoder failed mid-stream; reconnect for software decoding"
                );
                self.decode(chunk)
            }
            Err(error) => Err(error),
        }
    }
    fn decode(&mut self, chunk: &pb::VideoChunk) -> Result<Vec<Frame>> {
        ensure!(
            chunk.data.len() <= 16 * 1024 * 1024,
            "encoded frame exceeds limit"
        );
        ensure!(
            chunk.timestamp_micros <= i64::MAX as u64,
            "invalid frame timestamp"
        );
        unsafe {
            av::av_packet_unref(self.packet);
            check(
                av::av_new_packet(self.packet, chunk.data.len() as i32),
                "allocate packet payload",
            )?;
            ptr::copy_nonoverlapping(chunk.data.as_ptr(), (*self.packet).data, chunk.data.len());
            (*self.packet).pts = chunk.timestamp_micros as i64;
            (*self.packet).dts = chunk.timestamp_micros as i64;
            if chunk.keyframe {
                (*self.packet).flags |= av::AV_PKT_FLAG_KEY;
            }
            check(
                av::avcodec_send_packet(self.context, self.packet),
                "submit compressed frame",
            )?;
            self.waiting = false;
            let mut frames = Vec::new();
            loop {
                let frame = Frame::new()?;
                let result = av::avcodec_receive_frame(self.context, frame.0);
                if result == -11 || result == av::AVERROR_EOF {
                    break;
                }
                check(result, "decode frame")?;
                let f = &*frame.0;
                ensure!(
                    f.width > 0 && f.height > 0 && f.width <= 8192 && f.height <= 8192,
                    "unsupported decoded dimensions"
                );
                if self.hardware {
                    ensure!(
                        f.format == av::AVPixelFormat::AV_PIX_FMT_D3D11 as i32,
                        "hardware decoder returned a CPU frame"
                    );
                    self.label = "D3D11VA hardware decode".into();
                } else {
                    ensure!(
                        f.format == av::AVPixelFormat::AV_PIX_FMT_YUV420P as i32
                            || f.format == av::AVPixelFormat::AV_PIX_FMT_YUVJ420P as i32
                            || f.format == av::AVPixelFormat::AV_PIX_FMT_NV12 as i32,
                        "unsupported software pixel format {} (requires 8-bit 4:2:0)",
                        f.format
                    );
                }
                frames.push(frame);
                ensure!(frames.len() <= 16, "decoder output exceeded bounded batch");
            }
            Ok(frames)
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn packaged_decoders_expose_modern_d3d11_frames() {
        unsafe {
            for name in ["h264", "vp9", "av1"] {
                let name = CString::new(name).unwrap();
                let codec = av::avcodec_find_decoder_by_name(name.as_ptr());
                assert!(!codec.is_null());
                let mut supported = false;
                for i in 0..64 {
                    let config = av::avcodec_get_hw_config(codec, i);
                    if config.is_null() {
                        break;
                    }
                    supported |= (*config).device_type
                        == av::AVHWDeviceType::AV_HWDEVICE_TYPE_D3D11VA
                        && (*config).pix_fmt == av::AVPixelFormat::AV_PIX_FMT_D3D11;
                }
                assert!(
                    supported,
                    "missing D3D11 texture configuration for {name:?}"
                );
            }
            assert!(
                !av::avcodec_find_decoder_by_name(CString::new("libdav1d").unwrap().as_ptr())
                    .is_null()
            );
        }
    }
}
