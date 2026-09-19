//! Native Win32 shell; the video worker alone owns D3D11 (ADR-0243 §SD2/SD4).
mod decoder;
mod video;
use crate::{
    geometry::Viewport,
    input::{InputState, Modifiers, MouseButton, Utf16Assembler},
    session::{self, Command, Event},
    wire::pb,
};
use anyhow::{bail, Context, Result};
use std::{
    cell::RefCell,
    path::PathBuf,
    sync::{
        atomic::{AtomicBool, Ordering},
        mpsc::{self, Receiver, SyncSender},
        Arc,
    },
    thread,
    time::{Duration, Instant},
};
use video::{DecodeMode, Video};
use windows::{
    core::{w, PCWSTR},
    Win32::{
        Foundation::*,
        Graphics::Gdi::*,
        System::{DataExchange::*, LibraryLoader::GetModuleHandleW, Memory::*},
        UI::{
            Controls::WM_MOUSELEAVE, HiDpi::*, Input::KeyboardAndMouse::*, WindowsAndMessaging::*,
        },
    },
};

const CONNECT: usize = 101;
const DISCONNECT: usize = 102;
const TAKE: usize = 103;
const FULL: usize = 104;
const FIT: usize = 105;
const CLIP: usize = 106;
const PASTE: usize = 107;
const RESIZE_REMOTE: usize = 108;
thread_local! {static APP:RefCell<Option<App>>=const {RefCell::new(None)};}
fn wide(s: &str) -> Vec<u16> {
    s.encode_utf16().chain(Some(0)).collect()
}
pub fn show_error(text: &str) {
    unsafe {
        MessageBoxW(
            None,
            PCWSTR(wide(text).as_ptr()),
            w!("imzero2 viewer"),
            MB_OK | MB_ICONERROR,
        );
    }
}

struct App {
    window: HWND,
    video: HWND,
    endpoint: HWND,
    status: HWND,
    connect_button: HWND,
    // Commands are a tokio channel so the network worker sleeps on it rather
    // than polling; `try_send`/`blocking_send` need no runtime, so this side
    // stays an ordinary Win32 message loop. Events stay a std channel, drained
    // without blocking from WM_TIMER. See `session::spawn`.
    commands: tokio::sync::mpsc::Sender<Command>,
    events: Receiver<Event>,
    network: Option<thread::JoinHandle<()>>,
    video_commands: SyncSender<VideoCommand>,
    video_events: Receiver<VideoEvent>,
    video_thread: Option<thread::JoinHandle<()>>,
    video_stop: Arc<AtomicBool>,
    hello: Option<pb::SessionHello>,
    active: bool,
    connected: bool,
    desired: bool,
    fit: bool,
    clipboard: bool,
    fullscreen: Option<RECT>,
    // The whole input path lives in `crate::input`, which builds every wire
    // message and owns the held-key/button/modifier/focus state. This shell
    // translates a `WM_*` message into a raw transition and sends what comes
    // back; it keeps no parallel copy, so what the unit tests exercise is
    // what ships.
    input: InputState,
    text: Utf16Assembler,
    generation: u64,
    retry_at: Option<Instant>,
    retry_delay: Duration,
    last_progress: Instant,
    capture: Option<PathBuf>,
    exit_frames: Option<u64>,
    deadline: Option<Instant>,
    exit_code: i32,
    closing: bool,
    command_overflow: std::cell::Cell<bool>,
    awaiting_frame: bool,
    pending_reset: bool,
}
enum VideoCommand {
    Reset(u64),
    Hello(u64, pb::SessionHello),
    Frame(u64, pb::VideoChunk),
    Resize(u32, u32),
    Fit(bool),
    Capture(PathBuf),
}
enum VideoEvent {
    Status(u64, String, u64),
    Error(u64, String),
    Captured(u64),
}

pub fn run() -> Result<()> {
    let mut endpoint = String::new();
    let mut mode = DecodeMode::Auto;
    let mut capture = None;
    let mut exit_frames = None;
    let mut timeout = None;
    let mut args = std::env::args().skip(1);
    while let Some(arg) = args.next() {
        match arg.as_str(){
        "--url"=>endpoint=args.next().context("--url needs an endpoint")?,
        "--software"=>mode=DecodeMode::Software,"--hardware"=>mode=DecodeMode::Hardware,
        "--capture"=>capture=Some(PathBuf::from(args.next().context("--capture needs a BMP path")?)),
        "--exit-after-frames"=>exit_frames=Some(args.next().context("missing frame count")?.parse::<u64>()?),
        "--timeout"=>timeout=Some(Duration::from_secs(args.next().context("missing timeout")?.parse::<u64>()?)),
        _=>bail!("unknown option {arg}; options: --url URL --software --hardware --capture FILE.bmp --exit-after-frames N --timeout SECONDS"),
    }
    }
    unsafe {
        let _ = SetProcessDpiAwarenessContext(DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2);
        let instance: HINSTANCE = GetModuleHandleW(None)?.into();
        for (name, proc) in [
            (
                w!("ImzeroViewer"),
                Some(window_proc as unsafe extern "system" fn(_, _, _, _) -> _),
            ),
            (
                w!("ImzeroVideo"),
                Some(video_proc as unsafe extern "system" fn(_, _, _, _) -> _),
            ),
        ] {
            let class = WNDCLASSW {
                hInstance: instance,
                lpszClassName: name,
                lpfnWndProc: proc,
                hCursor: LoadCursorW(None, IDC_ARROW)?,
                hbrBackground: HBRUSH((COLOR_WINDOW.0 + 1) as usize as *mut _),
                ..Default::default()
            };
            if RegisterClassW(&class) == 0 {
                bail!("register window class: {}", std::io::Error::last_os_error());
            }
        }
        let window = CreateWindowExW(
            WINDOW_EX_STYLE::default(),
            w!("ImzeroViewer"),
            w!("imzero2 viewer"),
            WS_OVERLAPPEDWINDOW | WS_CLIPCHILDREN,
            CW_USEDEFAULT,
            CW_USEDEFAULT,
            1100,
            780,
            None,
            None,
            Some(instance),
            None,
        )?;
        let endpoint_control = CreateWindowExW(
            WS_EX_CLIENTEDGE,
            w!("EDIT"),
            PCWSTR(wide(&endpoint).as_ptr()),
            WS_CHILD | WS_VISIBLE | WS_TABSTOP | WINDOW_STYLE(ES_AUTOHSCROLL as u32),
            8,
            8,
            880,
            26,
            Some(window),
            None,
            Some(instance),
            None,
        )?;
        let connect_button = CreateWindowExW(
            WINDOW_EX_STYLE::default(),
            w!("BUTTON"),
            w!("Connect"),
            WS_CHILD | WS_VISIBLE | WS_TABSTOP,
            900,
            8,
            100,
            26,
            Some(window),
            Some(HMENU(CONNECT as *mut _)),
            Some(instance),
            None,
        )?;
        let status = CreateWindowExW(
            WINDOW_EX_STYLE::default(),
            w!("STATIC"),
            w!("Disconnected — enter a ws:// or wss:// endpoint"),
            WS_CHILD | WS_VISIBLE,
            8,
            720,
            1000,
            24,
            Some(window),
            None,
            Some(instance),
            None,
        )?;
        let video = CreateWindowExW(
            WINDOW_EX_STYLE::default(),
            w!("ImzeroVideo"),
            w!(""),
            WS_CHILD | WS_VISIBLE | WS_TABSTOP,
            0,
            42,
            1100,
            670,
            Some(window),
            None,
            Some(instance),
            None,
        )?;
        let menu = CreateMenu()?;
        for (id, title) in [
            (CONNECT, "Connect"),
            (DISCONNECT, "Disconnect"),
            (TAKE, "Take session"),
            (FULL, "Fullscreen (F11)"),
            (FIT, "Fit / 1:1"),
            (RESIZE_REMOTE, "Resize remote to window"),
            (CLIP, "Enable clipboard"),
            (PASTE, "Paste clipboard"),
        ] {
            AppendMenuW(menu, MF_STRING, id, PCWSTR(wide(title).as_ptr()))?;
        }
        SetMenu(window, Some(menu))?;
        let (commands, events, network) = session::spawn();
        let (video_commands, rx) = mpsc::sync_channel(32);
        let (tx, video_events) = mpsc::sync_channel(8);
        let video_stop = Arc::new(AtomicBool::new(false));
        let stop = video_stop.clone();
        let raw = video.0 as usize;
        let video_thread = thread::spawn(move || video_worker(raw, mode, rx, tx, stop));
        APP.with(|a| {
            *a.borrow_mut() = Some(App {
                window,
                video,
                endpoint: endpoint_control,
                status,
                connect_button,
                commands,
                events,
                network: Some(network),
                video_commands,
                video_events,
                video_thread: Some(video_thread),
                video_stop,
                hello: None,
                active: false,
                connected: false,
                desired: false,
                fit: true,
                clipboard: false,
                fullscreen: None,
                input: InputState::new(),
                text: Utf16Assembler::new(),
                generation: 0,
                retry_at: None,
                retry_delay: Duration::from_secs(1),
                last_progress: Instant::now(),
                capture,
                exit_frames,
                deadline: timeout.map(|t| Instant::now() + t),
                exit_code: 0,
                closing: false,
                command_overflow: std::cell::Cell::new(false),
                awaiting_frame: false,
                pending_reset: false,
            })
        });
        let _ = ShowWindow(window, SW_SHOW);
        SetTimer(Some(window), 1, 10, None);
        APP.with(|a| {
            if let Some(a) = a.borrow_mut().as_mut() {
                a.layout();
                if !endpoint.is_empty() {
                    a.connect();
                }
            }
        });
        let mut msg = MSG::default();
        loop {
            let result = GetMessageW(&mut msg, None, 0, 0).0;
            if result == -1 {
                bail!("Win32 message loop failed");
            }
            if result == 0 {
                break;
            }
            let _ = TranslateMessage(&msg);
            DispatchMessageW(&msg);
        }
        let code = APP.with(|a| {
            let mut slot = a.borrow_mut();
            let mut app = slot.take().unwrap();
            app.shutdown();
            app.exit_code
        });
        if code != 0 {
            bail!("scripted viewer run failed");
        }
    }
    Ok(())
}

fn video_worker(
    raw: usize,
    mode: DecodeMode,
    rx: Receiver<VideoCommand>,
    tx: SyncSender<VideoEvent>,
    stop: Arc<AtomicBool>,
) {
    let initial = match Video::new(HWND(raw as *mut _), mode) {
        Ok(v) => v,
        Err(e) => {
            let _ = tx.try_send(VideoEvent::Error(0, format!("{e:#}")));
            return;
        }
    };
    let mut v = Some(initial);
    let mut generation = 0;
    let mut failed = false;
    let mut terminal_event = None;
    let mut size = (1, 1);
    let mut fit = true;
    while !stop.load(Ordering::Acquire) {
        if let Some(event) = terminal_event.take() {
            match tx.try_send(event) {
                Ok(()) => {}
                Err(mpsc::TrySendError::Full(event)) => {
                    terminal_event = Some(event);
                    thread::sleep(Duration::from_millis(2));
                    continue;
                }
                Err(mpsc::TrySendError::Disconnected(_)) => break,
            }
        }
        let command = match rx.recv_timeout(Duration::from_millis(50)) {
            Ok(c) => c,
            Err(mpsc::RecvTimeoutError::Timeout) => continue,
            Err(_) => break,
        };
        let result = match command {
            VideoCommand::Reset(g) => {
                generation = g;
                if failed {
                    // A failed device/swap chain cannot be healed by decoder reset.
                    // Recreate on the owning worker while the UI keeps pumping.
                    drop(v.take());
                    match Video::new(HWND(raw as *mut _), mode) {
                        Ok(mut next) => {
                            next.set_fit(fit);
                            let result = next.resize(size.0, size.1);
                            v = Some(next);
                            failed = false;
                            result
                        }
                        Err(e) => Err(e),
                    }
                } else {
                    v.as_mut().unwrap().reset()
                }
            }
            VideoCommand::Hello(g, h) if g == generation && v.is_some() => {
                failed = false;
                v.as_mut().unwrap().configure(&h)
            }
            VideoCommand::Frame(g, f) if g == generation && !failed => v.as_mut().unwrap().push(&f),
            VideoCommand::Resize(w, h) => {
                size = (w, h);
                if let Some(v) = v.as_mut() {
                    v.resize(w, h)
                } else {
                    Ok(())
                }
            }
            VideoCommand::Fit(next_fit) => {
                fit = next_fit;
                if let Some(v) = v.as_mut() {
                    v.set_fit(fit);
                    v.repaint()
                } else {
                    Ok(())
                }
            }
            VideoCommand::Capture(path) if v.is_some() => {
                v.as_mut().unwrap().capture(&path).map(|_| {
                    terminal_event = Some(VideoEvent::Captured(generation));
                })
            }
            _ => Ok(()),
        };
        if let Err(e) = result {
            failed = true;
            terminal_event = Some(VideoEvent::Error(generation, format!("{e:#}")));
        } else if let Some(v) = v.as_ref() {
            let s = v.stats();
            let _ = tx.try_send(VideoEvent::Status(
                generation,
                format!(
                    "{} · GPU presentation: {} · {} frames",
                    s.decoder, s.adapter, s.frames
                ),
                s.frames,
            ));
        }
    }
}

impl App {
    fn status(&self, text: &str) {
        if self.deadline.is_some() {
            eprintln!("{text}");
        }
        unsafe {
            let _ = SetWindowTextW(self.status, PCWSTR(wide(text).as_ptr()));
        }
    }
    fn send(&self, command: Command) {
        if self.commands.try_send(command).is_err() {
            self.command_overflow.set(true);
            self.status("Network command backlog; cancelling the session");
        }
    }
    fn control(&self, control: pb::session_control::Control) {
        self.send(Command::Send(pb::SessionControl {
            control: Some(control),
        }));
    }
    /// Send one wire event built by [`InputState`]. Events are built
    /// unconditionally and dropped here when the session is not ours: the
    /// state tracking has to stay correct across a takeover either way, and
    /// ADR-0242 SD2 makes the *host* responsible for cancelling what a
    /// departing owner held.
    fn send_input(&self, event: pb::InputEvent) {
        if self.active && self.connected {
            self.send(Command::Input(event));
        }
    }
    /// Read the live modifier state from Win32 into [`InputState`], so the
    /// next event it builds carries the bitmask. Called at the top of every
    /// handler that produces input: `GetKeyState` reports the state as of
    /// the message being dispatched, not the state now.
    fn sync_modifiers(&mut self) {
        unsafe {
            self.input.set_modifiers(Modifiers::windows(
                GetKeyState(VK_MENU.0 as i32) < 0,
                GetKeyState(VK_CONTROL.0 as i32) < 0,
                GetKeyState(VK_SHIFT.0 as i32) < 0,
            ));
        }
    }
    fn reset_video(&mut self) {
        self.generation = self.generation.wrapping_add(1);
        self.awaiting_frame = false;
        self.pending_reset = self
            .video_commands
            .try_send(VideoCommand::Reset(self.generation))
            .is_err();
    }
    fn connect(&mut self) {
        let mut text = vec![0u16; 4096];
        let len = unsafe { GetWindowTextW(self.endpoint, &mut text) } as usize;
        let endpoint = String::from_utf16_lossy(&text[..len]);
        self.cancel_input();
        self.connected = false;
        self.reset_video();
        self.hello = None;
        self.active = false;
        self.desired = true;
        self.retry_at = None;
        self.last_progress = Instant::now();
        self.send(Command::Connect(endpoint));
        self.status("Connecting…");
    }
    fn disconnect(&mut self) {
        self.cancel_input();
        self.desired = false;
        self.retry_at = None;
        self.connected = false;
        self.active = false;
        self.hello = None;
        self.reset_video();
        self.send(Command::Disconnect);
        self.status("Disconnected");
    }
    /// Drop everything this connection was holding. Unlike [`Self::focus`]
    /// this clears unconditionally — a caller reaching for it is cancelling
    /// a session, not reporting a transition — and only the wire event is
    /// conditional on there having been one.
    fn cancel_input(&mut self) {
        let announce = self.input.is_focused();
        let event = self.input.focus(false);
        if announce {
            self.send_input(event);
        }
        self.text.reset();
        unsafe {
            let _ = ReleaseCapture();
        }
    }
    fn focus(&mut self, on: bool) {
        if self.input.is_focused() == on {
            return;
        }
        // `InputState::focus(false)` clears the held sets itself — locally
        // only, never as synthetic releases on the wire (ADR-0242 SD2).
        let event = self.input.focus(on);
        self.send_input(event);
    }
    fn enqueue_video(&mut self, c: VideoCommand) {
        if self.video_commands.try_send(c).is_err() {
            self.status("Video backlog exceeded limit; rejoining at next keyframe");
            self.send(Command::Disconnect);
            self.connected = false;
            self.retry_at = Some(Instant::now() + Duration::from_secs(1));
            self.reset_video();
        }
    }
    fn tick(&mut self) {
        if self.closing {
            // DXGI can synchronously message HWND during Present. Keep pumping until
            // the worker exits rather than joining it from WM_CLOSE.
            if self.video_thread.as_ref().is_none_or(|t| t.is_finished()) {
                unsafe {
                    let _ = PostMessageW(Some(self.window), WM_APP + 1, WPARAM(0), LPARAM(0));
                }
            }
            return;
        }
        if self.command_overflow.get() {
            // Clear the session flags first: cancel_input builds its focus
            // event as usual, and send_input drops it rather than pushing
            // onto the very channel that just overflowed.
            self.connected = false;
            self.active = false;
            self.cancel_input();
            if self.commands.try_send(Command::Disconnect).is_ok() {
                self.command_overflow.set(false);
                self.reset_video();
            }
            return;
        }
        if self.pending_reset
            && self
                .video_commands
                .try_send(VideoCommand::Reset(self.generation))
                .is_ok()
        {
            self.pending_reset = false;
        }
        if self.awaiting_frame && self.last_progress.elapsed() > Duration::from_secs(15) {
            self.status("Timed out waiting for a decodable keyframe; reconnecting");
            self.send(Command::Disconnect);
            self.awaiting_frame = false;
        }
        if self.deadline.is_some_and(|t| Instant::now() >= t) {
            self.exit_code = 1;
            unsafe {
                let _ = PostMessageW(Some(self.window), WM_CLOSE, WPARAM(0), LPARAM(0));
            }
            return;
        }
        for _ in 0..64 {
            let Ok(event) = self.events.try_recv() else {
                break;
            };
            match event {
                Event::Connected => {
                    self.connected = true;
                    self.send(Command::Send(session::client_hello("Windows viewer")));
                    self.last_progress = Instant::now();
                    self.status("Connected — waiting for stream and roster");
                }
                Event::Disconnected(reason) => {
                    self.cancel_input();
                    self.connected = false;
                    self.active = false;
                    self.hello = None;
                    self.reset_video();
                    self.status(&reason);
                    if self.desired {
                        self.retry_at = Some(Instant::now() + self.retry_delay);
                        self.retry_delay = (self.retry_delay * 2).min(Duration::from_secs(5));
                    }
                }
                Event::Hello(h) => {
                    if self.hello.as_ref().is_some_and(|old| {
                        old.width_px == h.width_px
                            && old.height_px == h.height_px
                            && old.pixels_per_point == h.pixels_per_point
                            && old.codec == h.codec
                    }) {
                        self.hello = Some(h);
                        continue;
                    }
                    self.reset_video();
                    self.hello = Some(h.clone());
                    self.enqueue_video(VideoCommand::Hello(self.generation, h));
                    self.caps();
                    self.awaiting_frame = true;
                    self.last_progress = Instant::now();
                }
                Event::Video(f) => {
                    if self.connected && self.hello.is_some() {
                        self.enqueue_video(VideoCommand::Frame(self.generation, f));
                    }
                }
                Event::Control(c) => match c.control {
                    Some(pb::session_control::Control::Roster(r)) => {
                        let active = r.you_role == 1;
                        if self.active && !active {
                            self.cancel_input();
                        }
                        let gained = active && !self.active;
                        self.active = active;
                        if gained {
                            self.caps();
                            let has_focus = unsafe { GetFocus() } == self.video;
                            // Gaining the role resets the host's view of this
                            // connection's focus, so re-announce it instead of
                            // suppressing it as an unchanged value.
                            let _ = self.input.focus(false);
                            self.focus(has_focus);
                        }
                        self.status(if active {
                            "Active session"
                        } else {
                            "Passive viewer — use Take session to control"
                        });
                    }
                    Some(pb::session_control::Control::Clipboard(c))
                        if self.clipboard && self.active =>
                    {
                        if let Err(e) = clipboard_write(self.window, &c.text) {
                            self.status(&e.to_string());
                        }
                    }
                    Some(pb::session_control::Control::CursorShape(c)) => unsafe {
                        let cursor = match c.shape {
                            1 => None,
                            4 => LoadCursorW(None, IDC_HAND).ok(),
                            9 | 10 => LoadCursorW(None, IDC_IBEAM).ok(),
                            5 | 6 => LoadCursorW(None, IDC_WAIT).ok(),
                            8 => LoadCursorW(None, IDC_CROSS).ok(),
                            19 | 31 => LoadCursorW(None, IDC_SIZEWE).ok(),
                            22 | 32 => LoadCursorW(None, IDC_SIZENS).ok(),
                            _ => LoadCursorW(None, IDC_ARROW).ok(),
                        };
                        SetCursor(cursor);
                    },
                    _ => {}
                },
            }
        }
        for _ in 0..16 {
            let Ok(event) = self.video_events.try_recv() else {
                break;
            };
            match event {
                VideoEvent::Status(g, status, frames) if g == self.generation => {
                    if self.connected {
                        self.status(&format!(
                            "{} · {status}",
                            if self.active { "Active" } else { "Passive" }
                        ));
                    }
                    if frames > 0 {
                        self.awaiting_frame = false;
                        self.last_progress = Instant::now();
                        self.retry_delay = Duration::from_secs(1);
                    }
                    if self.exit_frames.is_some_and(|n| frames >= n) {
                        self.exit_frames = None;
                        if let Some(path) = self.capture.take() {
                            self.enqueue_video(VideoCommand::Capture(path));
                        } else {
                            unsafe {
                                let _ =
                                    PostMessageW(Some(self.window), WM_CLOSE, WPARAM(0), LPARAM(0));
                            }
                        }
                    }
                }
                VideoEvent::Error(g, e) if g == 0 || g == self.generation => {
                    self.status(&e);
                    self.send(Command::Disconnect);
                    self.connected = false;
                    if self.desired {
                        self.retry_at = Some(Instant::now() + Duration::from_secs(2));
                    }
                    if self.deadline.is_some() {
                        self.exit_code = 1;
                    }
                }
                VideoEvent::Captured(g) if g == self.generation => unsafe {
                    let _ = PostMessageW(Some(self.window), WM_CLOSE, WPARAM(0), LPARAM(0));
                },
                _ => {}
            }
        }
        if self.retry_at.is_some_and(|t| Instant::now() >= t) && self.desired && !self.pending_reset
        {
            self.connect();
        }
    }
    fn caps(&self) {
        if self.active {
            if let Some(h) = &self.hello {
                self.send(Command::Send(session::capabilities(
                    h.width_px,
                    h.height_px,
                )));
            }
        }
    }
    fn layout(&mut self) {
        unsafe {
            let mut r = RECT::default();
            if GetClientRect(self.window, &mut r).is_err() {
                return;
            }
            let scale = GetDpiForWindow(self.window) as f32 / 96.0;
            let bar = (42.0 * scale) as i32;
            let bottom = (28.0 * scale) as i32;
            let full = self.fullscreen.is_some();
            for h in [self.endpoint, self.connect_button, self.status] {
                let _ = ShowWindow(h, if full { SW_HIDE } else { SW_SHOW });
            }
            let top = if full { 0 } else { bar };
            let height = (r.bottom - top - if full { 0 } else { bottom }).max(0);
            let _ = MoveWindow(self.video, 0, top, r.right, height, true);
            if !full {
                let button = (100.0 * scale) as i32;
                let _ = MoveWindow(
                    self.endpoint,
                    8,
                    8,
                    (r.right - button - 24).max(1),
                    bar - 16,
                    true,
                );
                let _ = MoveWindow(
                    self.connect_button,
                    (r.right - button - 8).max(0),
                    8,
                    button,
                    bar - 16,
                    true,
                );
                let _ = MoveWindow(
                    self.status,
                    8,
                    r.bottom - bottom,
                    r.right - 16,
                    bottom,
                    true,
                );
            }
            // Resize messages are coalescible; next WM_SIZE/timer can retry without losing video.
            let _ = self
                .video_commands
                .try_send(VideoCommand::Resize(r.right.max(0) as u32, height as u32));
        }
    }
    fn fullscreen(&mut self) {
        unsafe {
            if let Some(r) = self.fullscreen.take() {
                SetWindowLongPtrW(self.window, GWL_STYLE, WS_OVERLAPPEDWINDOW.0 as isize);
                let _ = SetWindowPos(
                    self.window,
                    None,
                    r.left,
                    r.top,
                    r.right - r.left,
                    r.bottom - r.top,
                    SWP_FRAMECHANGED | SWP_NOZORDER,
                );
            } else {
                let mut r = RECT::default();
                if GetWindowRect(self.window, &mut r).is_err() {
                    return;
                }
                let mut monitor = MONITORINFO {
                    cbSize: std::mem::size_of::<MONITORINFO>() as u32,
                    ..Default::default()
                };
                if !GetMonitorInfoW(
                    MonitorFromWindow(self.window, MONITOR_DEFAULTTONEAREST),
                    &mut monitor,
                )
                .as_bool()
                {
                    return;
                }
                self.fullscreen = Some(r);
                SetWindowLongPtrW(
                    self.window,
                    GWL_STYLE,
                    (WS_POPUP | WS_VISIBLE | WS_CLIPCHILDREN).0 as isize,
                );
                let r = monitor.rcMonitor;
                let _ = SetWindowPos(
                    self.window,
                    None,
                    r.left,
                    r.top,
                    r.right - r.left,
                    r.bottom - r.top,
                    SWP_FRAMECHANGED | SWP_NOZORDER,
                );
            }
            self.layout();
        }
    }
    fn position(&self, x: i32, y: i32) -> Option<(f32, f32)> {
        let h = self.hello.as_ref()?;
        let mut r = RECT::default();
        unsafe {
            GetClientRect(self.video, &mut r).ok()?;
        }
        Viewport::new(
            (r.right as u32, r.bottom as u32),
            (h.width_px, h.height_px),
            h.pixels_per_point,
            self.fit,
        )?
        .logical(x as f32, y as f32, self.input.any_button_held())
    }
    fn menu(&mut self, id: usize) {
        match id {
            CONNECT => self.connect(),
            DISCONNECT => self.disconnect(),
            TAKE => self.control(pb::session_control::Control::TakeSession(
                pb::TakeSession {},
            )),
            FULL => self.fullscreen(),
            FIT => {
                self.fit = !self.fit;
                self.enqueue_video(VideoCommand::Fit(self.fit));
            }
            CLIP => {
                self.clipboard = !self.clipboard;
                self.status(if self.clipboard {
                    "Clipboard enabled; paste is explicit"
                } else {
                    "Clipboard disabled"
                });
            }
            PASTE if self.clipboard && self.active => match clipboard_read(self.window) {
                Ok(text) => {
                    self.control(pb::session_control::Control::Clipboard(pb::ClipboardData {
                        text,
                    }))
                }
                Err(e) => self.status(&e.to_string()),
            },
            RESIZE_REMOTE if self.active => unsafe {
                let mut r = RECT::default();
                if GetClientRect(self.video, &mut r).is_ok() {
                    let scale = GetDpiForWindow(self.video) as f32 / 96.0;
                    self.control(pb::session_control::Control::ViewportResize(
                        pb::ViewportResize {
                            logical_width: r.right as f32 / scale,
                            logical_height: r.bottom as f32 / scale,
                            pixel_scale: scale,
                        },
                    ));
                }
            },
            _ => {}
        }
    }
    fn shutdown(&mut self) {
        self.cancel_input();
        self.video_stop.store(true, Ordering::Release);
        // Shutdown is the one place that may wait for channel space: the
        // message loop has already ended, and a worker that exited on its own
        // closes the channel, so this returns an error rather than hanging.
        let _ = self.commands.blocking_send(Command::Stop);
        if let Some(t) = self.network.take() {
            let _ = t.join();
        }
        if let Some(t) = self.video_thread.take() {
            let _ = t.join();
        }
    }
}

// Never hold a RefCell borrow through a synchronously reentrant Win32 call.
// Nested notifications are handled by DefWindowProc; timer/layout revisits state.
fn with_app(f: impl FnOnce(&mut App)) {
    APP.with(|a| {
        if let Ok(mut a) = a.try_borrow_mut() {
            if let Some(a) = a.as_mut() {
                f(a);
            }
        }
    });
}
unsafe extern "system" fn window_proc(hwnd: HWND, msg: u32, w: WPARAM, l: LPARAM) -> LRESULT {
    unsafe {
        match msg {
            WM_TIMER => {
                with_app(|a| a.tick());
                LRESULT(0)
            }
            WM_SIZE => {
                with_app(|a| a.layout());
                LRESULT(0)
            }
            WM_DPICHANGED => {
                let r = &*(l.0 as *const RECT);
                let _ = SetWindowPos(
                    hwnd,
                    None,
                    r.left,
                    r.top,
                    r.right - r.left,
                    r.bottom - r.top,
                    SWP_NOZORDER | SWP_NOACTIVATE,
                );
                with_app(|a| a.layout());
                LRESULT(0)
            }
            WM_COMMAND => {
                with_app(|a| a.menu(w.0 & 0xffff));
                LRESULT(0)
            }
            WM_CLOSE => {
                with_app(|a| {
                    a.cancel_input();
                    a.closing = true;
                    a.desired = false;
                    a.video_stop.store(true, Ordering::Release);
                    let _ = a.commands.try_send(Command::Stop);
                });
                LRESULT(0)
            }
            m if m == WM_APP + 1 => {
                let _ = DestroyWindow(hwnd);
                LRESULT(0)
            }
            WM_DESTROY => {
                PostQuitMessage(0);
                LRESULT(0)
            }
            _ => DefWindowProcW(hwnd, msg, w, l),
        }
    }
}
unsafe extern "system" fn video_proc(hwnd: HWND, msg: u32, w: WPARAM, l: LPARAM) -> LRESULT {
    unsafe {
        match msg {
            WM_ERASEBKGND => LRESULT(1),
            WM_SETFOCUS => {
                with_app(|a| a.focus(true));
                LRESULT(0)
            }
            WM_KILLFOCUS | WM_CANCELMODE => {
                with_app(|a| a.cancel_input());
                LRESULT(0)
            }
            WM_CAPTURECHANGED => {
                with_app(|a| {
                    if a.input.any_button_held() {
                        a.cancel_input();
                    }
                });
                LRESULT(0)
            }
            WM_MOUSEMOVE => {
                with_app(|a| {
                    if let Some((x, y)) = a.position(l.0 as i16 as i32, (l.0 >> 16) as i16 as i32) {
                        let event = a.input.mouse_move(x, y);
                        a.send_input(event);
                    }
                });
                let mut track = TRACKMOUSEEVENT {
                    cbSize: std::mem::size_of::<TRACKMOUSEEVENT>() as u32,
                    dwFlags: TME_LEAVE,
                    hwndTrack: hwnd,
                    dwHoverTime: 0,
                };
                let _ = TrackMouseEvent(&mut track);
                LRESULT(0)
            }
            WM_MOUSELEAVE => {
                with_app(|a| {
                    let event = a.input.pointer_gone();
                    a.send_input(event);
                });
                LRESULT(0)
            }
            WM_LBUTTONDOWN | WM_RBUTTONDOWN | WM_MBUTTONDOWN | WM_XBUTTONDOWN | WM_LBUTTONUP
            | WM_RBUTTONUP | WM_MBUTTONUP | WM_XBUTTONUP => {
                let down = matches!(
                    msg,
                    WM_LBUTTONDOWN | WM_RBUTTONDOWN | WM_MBUTTONDOWN | WM_XBUTTONDOWN
                );
                if down {
                    let _ = SetFocus(Some(hwnd));
                }
                // The X messages pack which side button in the high word;
                // an unknown one is dropped rather than guessed at.
                let button = match msg {
                    WM_LBUTTONDOWN | WM_LBUTTONUP => Some(MouseButton::Primary),
                    WM_RBUTTONDOWN | WM_RBUTTONUP => Some(MouseButton::Secondary),
                    WM_MBUTTONDOWN | WM_MBUTTONUP => Some(MouseButton::Middle),
                    _ => match (w.0 >> 16) as u16 {
                        XBUTTON1 => Some(MouseButton::Extra1),
                        XBUTTON2 => Some(MouseButton::Extra2),
                        _ => None,
                    },
                };
                if let Some(button) = button {
                    with_app(|a| {
                        if !a.active {
                            return;
                        }
                        let Some((x, y)) = a.position(l.0 as i16 as i32, (l.0 >> 16) as i16 as i32)
                        else {
                            return;
                        };
                        if down {
                            SetCapture(hwnd);
                        }
                        a.sync_modifiers();
                        let event = a.input.mouse_button(x, y, button, down);
                        a.send_input(event);
                        if !a.input.any_button_held() {
                            let _ = ReleaseCapture();
                        }
                    });
                }
                // MSDN: an application that processes WM_XBUTTON* returns TRUE.
                if matches!(msg, WM_XBUTTONDOWN | WM_XBUTTONUP) {
                    LRESULT(1)
                } else {
                    LRESULT(0)
                }
            }
            WM_MOUSEWHEEL | WM_MOUSEHWHEEL => {
                let delta = (w.0 >> 16) as i16 as f32 / 120.0 * 3.0;
                with_app(|a| {
                    a.sync_modifiers();
                    // unit 1 = lines (`MouseWheel.unit` doc comment).
                    let event = a.input.mouse_wheel(
                        if msg == WM_MOUSEHWHEEL { -delta } else { 0.0 },
                        if msg == WM_MOUSEWHEEL { delta } else { 0.0 },
                        1,
                    );
                    a.send_input(event);
                });
                LRESULT(0)
            }
            WM_KEYDOWN | WM_SYSKEYDOWN | WM_KEYUP | WM_SYSKEYUP => {
                if w.0 == VK_F4.0 as usize && GetKeyState(VK_MENU.0 as i32) < 0 {
                    let _ = PostMessageW(
                        Some(GetParent(hwnd).unwrap_or(hwnd)),
                        WM_CLOSE,
                        WPARAM(0),
                        LPARAM(0),
                    );
                    return LRESULT(0);
                }
                let down = msg == WM_KEYDOWN || msg == WM_SYSKEYDOWN;
                let repeat = l.0 & (1 << 30) != 0;
                if w.0 == VK_F11.0 as usize {
                    if down && !repeat {
                        with_app(|a| a.fullscreen());
                    }
                    return LRESULT(0);
                }
                with_app(|a| {
                    // key_name_from_vk is a pure mapping, so the key-up
                    // spelling matches the key-down's without the shell
                    // remembering it; InputState holds the held set.
                    let Some(key) = crate::input::key_name_from_vk(w.0 as u32) else {
                        return;
                    };
                    a.sync_modifiers();
                    // `code` is empty: the v1 host mapper reads only `key`
                    // (`input.proto`'s KeyEvent.code doc comment).
                    let event = a.input.key(key, "", down, down && repeat);
                    a.send_input(event);
                });
                LRESULT(0)
            }
            WM_CHAR => {
                with_app(|a| {
                    let unit = w.0 as u16;
                    // Control characters arrive as key events instead.
                    if unit < 32 {
                        return;
                    }
                    let Some(c) = a.text.push(unit) else {
                        return; // high surrogate; its low half completes it
                    };
                    a.sync_modifiers();
                    // AltGr commonly appears as Ctrl+Alt but still produces text.
                    let m = a.input.modifiers();
                    if !m.ctrl || m.alt {
                        let event = a.input.text(c.to_string());
                        a.send_input(event);
                    }
                });
                LRESULT(0)
            }
            _ => DefWindowProcW(hwnd, msg, w, l),
        }
    }
}
struct Clipboard;
impl Drop for Clipboard {
    fn drop(&mut self) {
        unsafe {
            let _ = CloseClipboard();
        }
    }
}
fn clipboard_read(hwnd: HWND) -> Result<String> {
    unsafe {
        OpenClipboard(Some(hwnd))?;
        let _close = Clipboard;
        let handle = GetClipboardData(13)?;
        let mem = HGLOBAL(handle.0);
        let count = GlobalSize(mem) / 2;
        anyhow::ensure!(count <= 1024 * 1024, "clipboard text too large");
        let ptr = GlobalLock(mem).cast::<u16>();
        anyhow::ensure!(!ptr.is_null(), "lock clipboard");
        let slice = std::slice::from_raw_parts(ptr, count);
        let end = slice.iter().position(|v| *v == 0).unwrap_or(count);
        let result = String::from_utf16_lossy(&slice[..end]);
        let _ = GlobalUnlock(mem);
        Ok(result)
    }
}
fn clipboard_write(hwnd: HWND, text: &str) -> Result<()> {
    unsafe {
        anyhow::ensure!(text.len() <= 1024 * 1024, "clipboard text too large");
        let text = wide(text);
        let mem = GlobalAlloc(GMEM_MOVEABLE, text.len() * 2)?;
        let ptr = GlobalLock(mem).cast::<u16>();
        if ptr.is_null() {
            let _ = GlobalFree(Some(mem));
            bail!("lock clipboard memory");
        }
        std::ptr::copy_nonoverlapping(text.as_ptr(), ptr, text.len());
        let _ = GlobalUnlock(mem);
        if let Err(e) = OpenClipboard(Some(hwnd)) {
            let _ = GlobalFree(Some(mem));
            return Err(e.into());
        }
        let _close = Clipboard;
        if let Err(e) =
            EmptyClipboard().and_then(|_| SetClipboardData(13, Some(HANDLE(mem.0))).map(|_| ()))
        {
            let _ = GlobalFree(Some(mem));
            return Err(e.into());
        }
        Ok(())
    }
}
