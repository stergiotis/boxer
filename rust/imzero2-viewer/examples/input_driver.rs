//! Scripted Win32 input smoke driver for a running viewer. No system-wide input.
#[cfg(windows)]
fn main() -> anyhow::Result<()> {
    use windows::{
        core::w,
        Win32::{Foundation::*, UI::WindowsAndMessaging::*},
    };
    unsafe {
        let main = FindWindowW(w!("ImzeroViewer"), None)?;
        let video = FindWindowExW(Some(main), None, w!("ImzeroVideo"), None)?;
        for (message, wparam, lparam) in [
            (WM_SETFOCUS, 0, 0),
            (WM_MOUSEMOVE, 0, (150 << 16) | 200),
            (WM_LBUTTONDOWN, 1, (150 << 16) | 200),
            (WM_LBUTTONUP, 0, (150 << 16) | 200),
            // A side button: which one rides in the high word of wParam.
            (WM_XBUTTONDOWN, (XBUTTON1 as usize) << 16, (150 << 16) | 200),
            (WM_XBUTTONUP, (XBUTTON1 as usize) << 16, (150 << 16) | 200),
            (WM_KEYDOWN, 65, 1),
            (WM_CHAR, 65, 1),
            (WM_KEYUP, 65, (1isize << 31) | (1isize << 30) | 1),
            // U+1F600 as Windows delivers it: two WM_CHARs, one per surrogate
            // half, which must reach the wire as a single character.
            (WM_CHAR, 0xD83D, 1),
            (WM_CHAR, 0xDE00, 1),
            (WM_KILLFOCUS, 0, 0),
        ] {
            PostMessageW(Some(video), message, WPARAM(wparam), LPARAM(lparam))?;
            std::thread::sleep(std::time::Duration::from_millis(60));
        }
        // Exercise native menu actions without depending on a desktop window manager.
        PostMessageW(Some(main), WM_COMMAND, WPARAM(104), LPARAM(0))?;
        std::thread::sleep(std::time::Duration::from_millis(150));
        PostMessageW(Some(main), WM_COMMAND, WPARAM(104), LPARAM(0))?;
        SetWindowPos(main, None, 0, 0, 900, 650, SWP_NOMOVE | SWP_NOZORDER)?;
        std::thread::sleep(std::time::Duration::from_millis(150));
        PostMessageW(Some(main), WM_CLOSE, WPARAM(0), LPARAM(0))?;
    }
    Ok(())
}
#[cfg(not(windows))]
fn main() {
    eprintln!("input_driver runs on Windows/Wine");
    std::process::exit(1);
}
