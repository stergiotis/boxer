pub const FUNC_PROC_ID_OFFSET: u32 = 0;

#[derive(Copy, Clone, Debug)]
pub struct BoolFlags(u8);
bitflags::bitflags! {
    impl BoolFlags: u8 {
        const TRUE = 1u8 << 0;
        const FALSE = 1u8 << 1;
        const TOGGLED = 1u8 << 2;
    }
}

#[derive(Copy, Clone, Debug)]
pub struct ResponseFlags(u32);
bitflags::bitflags! {
    impl ResponseFlags: u32 {
        const PRIMARY_CLICKED = 1u32 << 0;
        const SECONDARY_CLICKED = 1u32 << 1;
        const LONG_TOUCHED = 1u32 << 2;
        const MIDDLE_CLICKED = 1u32 << 3;
        const DOUBLE_CLICKED = 1u32 << 4;
        const TRIPLE_CLICKED = 1u32 << 5;
        const CLICKED_ELSEWHERE = 1u32 << 6;
        const ENABLED = 1u32 << 7;
        const HOVERED = 1u32 << 8;
        const CONTAINS_POINTER = 1u32 << 9;
        const HIGHLIGHTED = 1u32 << 10;
        const HAS_FOCUS = 1u32 << 11;
        const GAINED_FOCUS = 1u32 << 12;
        const LOST_FOCUS = 1u32 << 13;
        const DRAG_STARTED = 1u32 << 14;
        const DRAGGED = 1u32 << 15;
        const DRAG_STOPPED = 1u32 << 16;
        const IS_POINTER_BUTTON_DOWN_ON = 1u32 << 17;
        const CHANGED = 1u32 << 18;
        const SHOULD_CLOSE = 1u32 << 19;
        const IS_TOOLTIP_OPEN = 1u32 << 20;
        // WINDOW_TOPMOST: this block's Area is the top layer of egui's
        // Middle order — the shell notion of "the active window". Set only
        // by the Window apply arm (it is a fact about the window's layer,
        // not about a response), so populate() below does not touch it.
        const WINDOW_TOPMOST = 1u32 << 21;

        // Bit 30 is FREE. It was NODELIKE_SELECTED, the egui_ltreeview
        // binding's only read-back, retired with the binding in ADR-0176.
        // Left as a hole rather than reused: the flags are a wire contract
        // with Go, and a bit that changes meaning is the kind of change that
        // compiles on both sides and lies at runtime.
        const BLOCK_SKIPPED = 1u32 << 31;
    }
}

impl ResponseFlags {
    pub fn populate(&mut self, resp: &egui::response::Response) {
        self.set(Self::PRIMARY_CLICKED, resp.clicked());
        self.set(Self::SECONDARY_CLICKED, resp.secondary_clicked());
        self.set(Self::LONG_TOUCHED, resp.long_touched());
        self.set(Self::MIDDLE_CLICKED, resp.middle_clicked());
        self.set(Self::DOUBLE_CLICKED, resp.double_clicked());
        self.set(Self::TRIPLE_CLICKED, resp.triple_clicked());
        self.set(Self::CLICKED_ELSEWHERE, resp.clicked_elsewhere());
        self.set(Self::ENABLED, resp.enabled());
        self.set(Self::HOVERED, resp.hovered());
        self.set(Self::CONTAINS_POINTER, resp.contains_pointer());
        self.set(Self::HIGHLIGHTED, resp.highlighted());
        self.set(Self::HAS_FOCUS, resp.has_focus());
        self.set(Self::GAINED_FOCUS, resp.gained_focus());
        self.set(Self::LOST_FOCUS, resp.lost_focus());
        self.set(Self::DRAG_STARTED, resp.drag_started());
        self.set(Self::DRAGGED, resp.dragged());
        self.set(Self::DRAG_STOPPED, resp.drag_stopped());
        self.set(
            Self::IS_POINTER_BUTTON_DOWN_ON,
            resp.is_pointer_button_down_on(),
        );
        self.set(Self::CHANGED, resp.changed());
        self.set(Self::SHOULD_CLOSE, resp.should_close());
        self.set(Self::IS_TOOLTIP_OPEN, resp.is_tooltip_open());
    }
    pub fn match_response_any(&self, resp: &egui::response::Response) -> bool {
        if self.contains(Self::PRIMARY_CLICKED) && resp.clicked() {
            return true;
        }
        if self.contains(Self::SECONDARY_CLICKED) && resp.secondary_clicked() {
            return true;
        }
        if self.contains(Self::LONG_TOUCHED) && resp.long_touched() {
            return true;
        }
        if self.contains(Self::MIDDLE_CLICKED) && resp.middle_clicked() {
            return true;
        }
        if self.contains(Self::DOUBLE_CLICKED) && resp.double_clicked() {
            return true;
        }
        if self.contains(Self::TRIPLE_CLICKED) && resp.triple_clicked() {
            return true;
        }
        if self.contains(Self::CLICKED_ELSEWHERE) && resp.clicked_elsewhere() {
            return true;
        }
        if self.contains(Self::ENABLED) && resp.enabled() {
            return true;
        }
        if self.contains(Self::HOVERED) && resp.hovered() {
            return true;
        }
        if self.contains(Self::CONTAINS_POINTER) && resp.contains_pointer() {
            return true;
        }
        if self.contains(Self::HIGHLIGHTED) && resp.highlighted() {
            return true;
        }
        if self.contains(Self::HAS_FOCUS) && resp.has_focus() {
            return true;
        }
        if self.contains(Self::GAINED_FOCUS) && resp.gained_focus() {
            return true;
        }
        if self.contains(Self::LOST_FOCUS) && resp.lost_focus() {
            return true;
        }
        if self.contains(Self::DRAG_STARTED) && resp.drag_started() {
            return true;
        }
        if self.contains(Self::DRAGGED) && resp.dragged() {
            return true;
        }
        if self.contains(Self::DRAG_STOPPED) && resp.drag_stopped() {
            return true;
        }
        if self.contains(Self::IS_POINTER_BUTTON_DOWN_ON) && resp.is_pointer_button_down_on() {
            return true;
        }
        if self.contains(Self::CHANGED) && resp.changed() {
            return true;
        }
        if self.contains(Self::SHOULD_CLOSE) && resp.should_close() {
            return true;
        }
        if self.contains(Self::IS_TOOLTIP_OPEN) && resp.is_tooltip_open() {
            return true;
        }
        false
    }
}
