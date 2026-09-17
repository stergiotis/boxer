//! GPU-resident video presentation, owned by one worker (ADR-0243 §SD3/SD4).
pub use super::decoder::DecodeMode;
use super::decoder::{Decoder, Frame};
use crate::{
    geometry::Viewport,
    wire::{pb, Codec},
};
use anyhow::{anyhow, bail, ensure, Context, Result};
use ffmpeg_sys_next as av;
use std::{
    ffi::CString,
    time::{Duration, Instant},
};
use windows::{
    core::{Interface, PCSTR},
    Win32::{
        Foundation::{HMODULE, HWND},
        Graphics::{Direct3D::Fxc::*, Direct3D::*, Direct3D11::*, Dxgi::Common::*, Dxgi::*},
    },
};

pub struct Stats {
    pub decoder: String,
    pub adapter: String,
    pub frames: u64,
}
pub struct Video {
    decoder: Option<Decoder>,
    device: ID3D11Device,
    context: ID3D11DeviceContext,
    swap: IDXGISwapChain1,
    target: Option<ID3D11RenderTargetView>,
    vertex: ID3D11VertexShader,
    pixel: ID3D11PixelShader,
    sampler: ID3D11SamplerState,
    constants: ID3D11Buffer,
    completion: ID3D11Query,
    textures: Vec<ID3D11Texture2D>,
    views: Vec<Option<ID3D11ShaderResourceView>>,
    layout: (u32, u32, bool),
    hello: Option<pb::SessionHello>,
    size: (u32, u32),
    fit: bool,
    mode: DecodeMode,
    adapter: String,
    frames: u64,
    last: Option<Frame>,
    suppress_present: bool,
}

fn shader(entry: &str, target: &str) -> Result<Vec<u8>> {
    let source = include_str!("video.hlsl");
    let entry = CString::new(entry)?;
    let target = CString::new(target)?;
    let mut code = None;
    let mut errors = None;
    unsafe {
        let result = D3DCompile(
            source.as_ptr().cast(),
            source.len(),
            PCSTR::null(),
            None,
            None,
            PCSTR(entry.as_ptr().cast()),
            PCSTR(target.as_ptr().cast()),
            D3DCOMPILE_ENABLE_STRICTNESS,
            0,
            &mut code,
            Some(&mut errors),
        );
        if let Err(e) = result {
            let detail = errors
                .map(|b| {
                    String::from_utf8_lossy(std::slice::from_raw_parts(
                        b.GetBufferPointer().cast::<u8>(),
                        b.GetBufferSize(),
                    ))
                    .into_owned()
                })
                .unwrap_or_default();
            bail!("D3DCompile failed: {e}: {detail}");
        }
        let b = code.context("shader compiler returned no bytecode")?;
        Ok(
            std::slice::from_raw_parts(b.GetBufferPointer().cast::<u8>(), b.GetBufferSize())
                .to_vec(),
        )
    }
}

impl Video {
    pub fn new(hwnd: HWND, mode: DecodeMode) -> Result<Self> {
        unsafe {
            let factory: IDXGIFactory2 = CreateDXGIFactory1()?;
            let mut selected = None;
            for index in 0..32 {
                let Ok(adapter) = factory.EnumAdapters1(index) else {
                    break;
                };
                let desc = adapter.GetDesc1()?;
                if desc.Flags & DXGI_ADAPTER_FLAG_SOFTWARE.0 as u32 != 0 {
                    continue;
                }
                let mut device = None;
                let mut context = None;
                if D3D11CreateDevice(
                    &adapter,
                    D3D_DRIVER_TYPE_UNKNOWN,
                    HMODULE::default(),
                    D3D11_CREATE_DEVICE_BGRA_SUPPORT | D3D11_CREATE_DEVICE_VIDEO_SUPPORT,
                    Some(&[D3D_FEATURE_LEVEL_11_0]),
                    D3D11_SDK_VERSION,
                    Some(&mut device),
                    None,
                    Some(&mut context),
                )
                .is_ok()
                {
                    let end = desc
                        .Description
                        .iter()
                        .position(|v| *v == 0)
                        .unwrap_or(desc.Description.len());
                    selected = Some((
                        device.unwrap(),
                        context.unwrap(),
                        String::from_utf16_lossy(&desc.Description[..end]),
                    ));
                    break;
                }
            }
            let (device, context, adapter) =
                selected.context("no hardware D3D11 adapter supports video presentation")?;
            // FFmpeg/driver internals can use worker threads even though this
            // application's decode and render submission are serialized.
            if let Ok(protection) = context.cast::<ID3D11Multithread>() {
                let _ = protection.SetMultithreadProtected(true);
            }
            let desc = DXGI_SWAP_CHAIN_DESC1 {
                Width: 1,
                Height: 1,
                Format: DXGI_FORMAT_B8G8R8A8_UNORM,
                SampleDesc: DXGI_SAMPLE_DESC {
                    Count: 1,
                    Quality: 0,
                },
                BufferUsage: DXGI_USAGE_RENDER_TARGET_OUTPUT,
                BufferCount: 2,
                SwapEffect: DXGI_SWAP_EFFECT_FLIP_DISCARD,
                Scaling: DXGI_SCALING_STRETCH,
                AlphaMode: DXGI_ALPHA_MODE_IGNORE,
                ..Default::default()
            };
            let swap = factory.CreateSwapChainForHwnd(&device, hwnd, &desc, None, None)?;
            factory.MakeWindowAssociation(hwnd, DXGI_MWA_NO_ALT_ENTER)?;
            let dxgi_device: IDXGIDevice1 = device.cast()?;
            dxgi_device.SetMaximumFrameLatency(1)?;
            let mut vertex = None;
            device.CreateVertexShader(&shader("vs_main", "vs_5_0")?, None, Some(&mut vertex))?;
            let mut pixel = None;
            device.CreatePixelShader(&shader("ps_main", "ps_5_0")?, None, Some(&mut pixel))?;
            let mut sampler = None;
            device.CreateSamplerState(
                &D3D11_SAMPLER_DESC {
                    Filter: D3D11_FILTER_MIN_MAG_LINEAR_MIP_POINT,
                    AddressU: D3D11_TEXTURE_ADDRESS_CLAMP,
                    AddressV: D3D11_TEXTURE_ADDRESS_CLAMP,
                    AddressW: D3D11_TEXTURE_ADDRESS_CLAMP,
                    MaxLOD: f32::MAX,
                    ComparisonFunc: D3D11_COMPARISON_NEVER,
                    ..Default::default()
                },
                Some(&mut sampler),
            )?;
            let mut constants = None;
            device.CreateBuffer(
                &D3D11_BUFFER_DESC {
                    ByteWidth: 64,
                    Usage: D3D11_USAGE_DEFAULT,
                    BindFlags: D3D11_BIND_CONSTANT_BUFFER.0 as u32,
                    ..Default::default()
                },
                None,
                Some(&mut constants),
            )?;
            let mut completion = None;
            device.CreateQuery(
                &D3D11_QUERY_DESC {
                    Query: D3D11_QUERY_EVENT,
                    MiscFlags: 0,
                },
                Some(&mut completion),
            )?;
            let mut v = Self {
                decoder: None,
                device,
                context,
                swap,
                target: None,
                vertex: vertex.unwrap(),
                pixel: pixel.unwrap(),
                sampler: sampler.unwrap(),
                constants: constants.unwrap(),
                completion: completion.unwrap(),
                textures: Vec::new(),
                views: Vec::new(),
                layout: (0, 0, false),
                hello: None,
                size: (1, 1),
                fit: true,
                mode,
                adapter,
                frames: 0,
                last: None,
                suppress_present: false,
            };
            v.target = Some(v.make_target()?);
            Ok(v)
        }
    }
    fn make_target(&self) -> Result<ID3D11RenderTargetView> {
        unsafe {
            let texture: ID3D11Texture2D = self.swap.GetBuffer(0)?;
            let mut view = None;
            self.device
                .CreateRenderTargetView(&texture, None, Some(&mut view))?;
            Ok(view.unwrap())
        }
    }
    pub fn reset(&mut self) -> Result<()> {
        self.finish()?;
        self.last = None;
        self.decoder = None;
        self.hello = None;
        self.frames = 0;
        Ok(())
    }
    pub fn configure(&mut self, hello: &pb::SessionHello) -> Result<()> {
        ensure!(
            hello.width_px > 0
                && hello.height_px > 0
                && hello.width_px <= 8192
                && hello.height_px <= 8192,
            "stream dimensions outside supported limit"
        );
        ensure!(
            hello.pixels_per_point.is_finite() && hello.pixels_per_point > 0.0,
            "invalid stream pixel scale"
        );
        let codec = Codec::from_hello(&hello.codec)?;
        self.finish()?;
        self.last = None;
        self.decoder = Some(Decoder::new(&self.device, codec, self.mode)?);
        self.hello = Some(hello.clone());
        self.frames = 0;
        Ok(())
    }
    pub fn set_fit(&mut self, fit: bool) {
        self.fit = fit;
    }
    pub fn stats(&self) -> Stats {
        Stats {
            decoder: self
                .decoder
                .as_ref()
                .map(|d| d.label.clone())
                .unwrap_or_else(|| "waiting for stream".into()),
            adapter: self.adapter.clone(),
            frames: self.frames,
        }
    }
    pub fn resize(&mut self, width: u32, height: u32) -> Result<()> {
        if width == 0 || height == 0 {
            self.size = (width, height);
            return Ok(());
        }
        if self.size == (width, height) {
            return Ok(());
        }
        self.finish()?;
        unsafe {
            self.context.OMSetRenderTargets(None, None);
            self.target = None;
            self.swap.ResizeBuffers(
                0,
                width,
                height,
                DXGI_FORMAT_UNKNOWN,
                DXGI_SWAP_CHAIN_FLAG(0),
            )?;
        }
        self.target = Some(self.make_target()?);
        self.size = (width, height);
        self.repaint()
    }
    pub fn push(&mut self, chunk: &pb::VideoChunk) -> Result<()> {
        let decoder = self
            .decoder
            .as_mut()
            .context("video arrived before stream hello")?;
        let frames = decoder.push(chunk)?;
        for frame in frames {
            self.finish()?;
            self.last = Some(frame);
            self.frames += 1;
        }
        self.repaint()
    }
    pub fn repaint(&mut self) -> Result<()> {
        if self.size.0 == 0 || self.size.1 == 0 || self.last.is_none() {
            return Ok(());
        }
        // Owned frame stays live through finish(), including any decoder-surface copy.
        let frame = self.last.as_ref().unwrap().0;
        unsafe {
            let f = &*frame;
            ensure!(
                f.crop_left == 0 && f.crop_top == 0 && f.crop_right == 0 && f.crop_bottom == 0,
                "nonzero decoder crop not supported"
            );
            let hardware = f.format == av::AVPixelFormat::AV_PIX_FMT_D3D11 as i32;
            let nv12 = hardware || f.format == av::AVPixelFormat::AV_PIX_FMT_NV12 as i32;
            let mut width = f.width as u32;
            let mut height = f.height as u32;
            let source = if hardware {
                ensure!(!f.data[0].is_null(), "missing hardware texture");
                let texture = ID3D11Texture2D::from_raw_borrowed(&f.data[0].cast())
                    .context("invalid hardware texture")?
                    .clone();
                let mut desc = Default::default();
                texture.GetDesc(&mut desc);
                ensure!(
                    desc.Format == DXGI_FORMAT_NV12,
                    "hardware surface is not 8-bit NV12"
                );
                width = desc.Width;
                height = desc.Height;
                Some(texture)
            } else {
                None
            };
            self.prepare(width, height, nv12)?;
            self.context
                .PSSetShaderResources(0, Some(&[None, None, None]));
            if let Some(source) = source {
                self.context.CopySubresourceRegion(
                    &self.textures[0],
                    0,
                    0,
                    0,
                    0,
                    &source,
                    f.data[1] as usize as u32,
                    None,
                );
            } else if nv12 {
                // NV12 software frame may have independent pitches; pack it without RGB conversion.
                let mut packed = vec![0u8; (width * height + width * height.div_ceil(2)) as usize];
                copy_plane(
                    &mut packed[..(width * height) as usize],
                    f.data[0],
                    f.linesize[0],
                    width,
                    height,
                )?;
                copy_plane(
                    &mut packed[(width * height) as usize..],
                    f.data[1],
                    f.linesize[1],
                    width,
                    height.div_ceil(2),
                )?;
                self.context.UpdateSubresource(
                    &self.textures[0],
                    0,
                    None,
                    packed.as_ptr().cast(),
                    width,
                    0,
                );
            } else {
                for i in 0..3 {
                    let w = if i == 0 { width } else { width.div_ceil(2) };
                    let h = if i == 0 { height } else { height.div_ceil(2) };
                    let mut packed = vec![0u8; (w * h) as usize];
                    copy_plane(&mut packed, f.data[i], f.linesize[i], w, h)?;
                    self.context.UpdateSubresource(
                        &self.textures[i],
                        0,
                        None,
                        packed.as_ptr().cast(),
                        w,
                        0,
                    );
                }
            }
            let params = color_matrix(f, nv12, width, height)?;
            self.context
                .UpdateSubresource(&self.constants, 0, None, params.as_ptr().cast(), 0, 0);
            let target = self.target.as_ref().context("missing render target")?;
            self.context
                .ClearRenderTargetView(target, &[0.0, 0.0, 0.0, 1.0]);
            self.context
                .OMSetRenderTargets(Some(&[Some(target.clone())]), None);
            let hello = self.hello.as_ref().unwrap();
            let viewport = Viewport::new(
                self.size,
                (f.width as u32, f.height as u32),
                hello.pixels_per_point,
                self.fit,
            )
            .context("invalid viewport")?;
            self.context.RSSetViewports(Some(&[D3D11_VIEWPORT {
                TopLeftX: viewport.x,
                TopLeftY: viewport.y,
                Width: viewport.width,
                Height: viewport.height,
                MinDepth: 0.0,
                MaxDepth: 1.0,
            }]));
            self.context
                .IASetPrimitiveTopology(D3D_PRIMITIVE_TOPOLOGY_TRIANGLELIST);
            self.context.VSSetShader(&self.vertex, None);
            self.context.PSSetShader(&self.pixel, None);
            self.context
                .PSSetSamplers(0, Some(&[Some(self.sampler.clone())]));
            self.context
                .PSSetConstantBuffers(0, Some(&[Some(self.constants.clone())]));
            self.context.PSSetShaderResources(0, Some(&self.views));
            self.context.Draw(3, 0);
            self.context
                .PSSetShaderResources(0, Some(&[None, None, None]));
            self.finish()?;
            if !self.suppress_present {
                self.swap.Present(1, DXGI_PRESENT(0)).ok()?;
            }
        }
        Ok(())
    }
    fn prepare(&mut self, width: u32, height: u32, nv12: bool) -> Result<()> {
        unsafe {
            if self.layout == (width, height, nv12) {
                return Ok(());
            }
            self.finish()?;
            self.textures.clear();
            self.views.clear();
            for i in 0..if nv12 { 1 } else { 3 } {
                let desc = D3D11_TEXTURE2D_DESC {
                    Width: if i == 0 { width } else { width.div_ceil(2) },
                    Height: if i == 0 { height } else { height.div_ceil(2) },
                    MipLevels: 1,
                    ArraySize: 1,
                    Format: if nv12 {
                        DXGI_FORMAT_NV12
                    } else {
                        DXGI_FORMAT_R8_UNORM
                    },
                    SampleDesc: DXGI_SAMPLE_DESC {
                        Count: 1,
                        Quality: 0,
                    },
                    Usage: D3D11_USAGE_DEFAULT,
                    BindFlags: D3D11_BIND_SHADER_RESOURCE.0 as u32,
                    ..Default::default()
                };
                let mut texture = None;
                self.device
                    .CreateTexture2D(&desc, None, Some(&mut texture))?;
                let texture = texture.unwrap();
                for format in if nv12 {
                    vec![DXGI_FORMAT_R8_UNORM, DXGI_FORMAT_R8G8_UNORM]
                } else {
                    vec![DXGI_FORMAT_R8_UNORM]
                } {
                    let desc = D3D11_SHADER_RESOURCE_VIEW_DESC {
                        Format: format,
                        ViewDimension: D3D_SRV_DIMENSION_TEXTURE2D,
                        Anonymous: D3D11_SHADER_RESOURCE_VIEW_DESC_0 {
                            Texture2D: D3D11_TEX2D_SRV {
                                MostDetailedMip: 0,
                                MipLevels: 1,
                            },
                        },
                    };
                    let mut view = None;
                    self.device
                        .CreateShaderResourceView(&texture, Some(&desc), Some(&mut view))?;
                    self.views.push(view);
                }
                self.textures.push(texture);
            }
            if nv12 {
                self.views.push(None);
            }
            self.layout = (width, height, nv12);
            Ok(())
        }
    }
    fn finish(&self) -> Result<()> {
        unsafe {
            self.context.End(&self.completion);
            self.context.Flush();
            let start = Instant::now();
            let mut done = 0u32;
            while done == 0 {
                self.context.GetData(
                    &self.completion,
                    Some((&mut done as *mut u32).cast()),
                    4,
                    0,
                )?;
                ensure!(
                    start.elapsed() < Duration::from_secs(2),
                    "GPU completion timeout"
                );
                if done == 0 {
                    std::thread::sleep(Duration::from_millis(1));
                }
            }
            Ok(())
        }
    }
    pub fn capture(&mut self, path: &std::path::Path) -> Result<()> {
        ensure!(self.last.is_some(), "no decoded frame to capture");
        // Flip-discard makes the old backbuffer undefined. Redraw without Present.
        self.suppress_present = true;
        let redraw = self.repaint();
        self.suppress_present = false;
        redraw?;
        unsafe {
            self.finish()?;
            let source: ID3D11Texture2D = self.swap.GetBuffer(0)?;
            let mut desc = Default::default();
            source.GetDesc(&mut desc);
            desc.Usage = D3D11_USAGE_STAGING;
            desc.BindFlags = 0;
            desc.CPUAccessFlags = D3D11_CPU_ACCESS_READ.0 as u32;
            desc.MiscFlags = 0;
            let mut staging = None;
            self.device
                .CreateTexture2D(&desc, None, Some(&mut staging))?;
            let staging = staging.unwrap();
            self.context.CopyResource(&staging, &source);
            self.finish()?;
            let mut mapped = D3D11_MAPPED_SUBRESOURCE::default();
            self.context
                .Map(&staging, 0, D3D11_MAP_READ, 0, Some(&mut mapped))?;
            let mut pixels = Vec::with_capacity((desc.Width * desc.Height * 4) as usize);
            for y in 0..desc.Height {
                pixels.extend_from_slice(std::slice::from_raw_parts(
                    mapped
                        .pData
                        .cast::<u8>()
                        .add((y * mapped.RowPitch) as usize),
                    (desc.Width * 4) as usize,
                ));
            }
            self.context.Unmap(&staging, 0);
            let mut bytes = Vec::new();
            bytes.extend_from_slice(b"BM");
            bytes.extend_from_slice(&(54u32 + pixels.len() as u32).to_le_bytes());
            bytes.extend_from_slice(&[0; 4]);
            bytes.extend_from_slice(&54u32.to_le_bytes());
            bytes.extend_from_slice(&40u32.to_le_bytes());
            bytes.extend_from_slice(&(desc.Width as i32).to_le_bytes());
            bytes.extend_from_slice(&(-(desc.Height as i32)).to_le_bytes());
            bytes.extend_from_slice(&1u16.to_le_bytes());
            bytes.extend_from_slice(&32u16.to_le_bytes());
            bytes.extend_from_slice(&[0; 24]);
            bytes.extend(pixels);
            std::fs::write(path, bytes)?;
            Ok(())
        }
    }
}

unsafe fn copy_plane(
    out: &mut [u8],
    source: *const u8,
    stride: i32,
    width: u32,
    height: u32,
) -> Result<()> {
    unsafe {
        ensure!(
            !source.is_null() && stride.unsigned_abs() >= width,
            "invalid decoded plane stride"
        );
        for y in 0..height {
            let row = source.offset(y as isize * stride as isize);
            out[(y * width) as usize..((y + 1) * width) as usize]
                .copy_from_slice(std::slice::from_raw_parts(row, width as usize));
        }
        Ok(())
    }
}
fn color_matrix(f: &av::AVFrame, nv12: bool, width: u32, height: u32) -> Result<[f32; 16]> {
    let (kr, kb) = match f.colorspace {
        av::AVColorSpace::AVCOL_SPC_BT709 => (0.2126f32, 0.0722f32),
        av::AVColorSpace::AVCOL_SPC_BT2020_NCL => (0.2627, 0.0593),
        av::AVColorSpace::AVCOL_SPC_BT470BG | av::AVColorSpace::AVCOL_SPC_SMPTE170M => {
            (0.299, 0.114)
        }
        av::AVColorSpace::AVCOL_SPC_UNSPECIFIED => {
            if f.height > 576 {
                (0.2126, 0.0722)
            } else {
                (0.299, 0.114)
            }
        }
        _ => return Err(anyhow!("unsupported video color matrix {:?}", f.colorspace)),
    };
    let full = f.color_range == av::AVColorRange::AVCOL_RANGE_JPEG
        || f.format == av::AVPixelFormat::AV_PIX_FMT_YUVJ420P as i32;
    let ys = if full { 1.0 } else { 255.0 / 219.0 };
    let cs = if full { 1.0 } else { 255.0 / 224.0 };
    let yo = if full { 0.0 } else { 16.0 / 255.0 };
    let co = 128.0 / 255.0;
    let r = 2.0 * (1.0 - kr) * cs;
    let b = 2.0 * (1.0 - kb) * cs;
    let gu = -2.0 * kb * (1.0 - kb) / (1.0 - kr - kb) * cs;
    let gv = -2.0 * kr * (1.0 - kr) / (1.0 - kr - kb) * cs;
    Ok([
        ys,
        0.0,
        r,
        -ys * yo - r * co,
        ys,
        gu,
        gv,
        -ys * yo - (gu + gv) * co,
        ys,
        b,
        0.0,
        -ys * yo - b * co,
        if nv12 { 1.0 } else { 0.0 },
        f.width as f32 / width as f32,
        f.height as f32 / height as f32,
        0.0,
    ])
}
