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

        const NODELIKE_SELECTED = 1u32 << 30;
        const BLOCK_SKIPPED = 1u32 << 31;
    }
}

impl ResponseFlags {
    pub fn populate(&mut self, resp: &egui::response::Response) {
        self.set(ResponseFlags::PRIMARY_CLICKED, resp.clicked());
        self.set(ResponseFlags::SECONDARY_CLICKED, resp.secondary_clicked());
        self.set(ResponseFlags::LONG_TOUCHED, resp.long_touched());
        self.set(ResponseFlags::MIDDLE_CLICKED, resp.middle_clicked());
        self.set(ResponseFlags::DOUBLE_CLICKED, resp.double_clicked());
        self.set(ResponseFlags::TRIPLE_CLICKED, resp.triple_clicked());
        self.set(ResponseFlags::CLICKED_ELSEWHERE, resp.clicked_elsewhere());
        self.set(ResponseFlags::ENABLED, resp.enabled());
        self.set(ResponseFlags::HOVERED, resp.hovered());
        self.set(ResponseFlags::CONTAINS_POINTER, resp.contains_pointer());
        self.set(ResponseFlags::HIGHLIGHTED, resp.highlighted());
        self.set(ResponseFlags::HAS_FOCUS, resp.has_focus());
        self.set(ResponseFlags::GAINED_FOCUS, resp.gained_focus());
        self.set(ResponseFlags::LOST_FOCUS, resp.lost_focus());
        self.set(ResponseFlags::DRAG_STARTED, resp.drag_started());
        self.set(ResponseFlags::DRAGGED, resp.dragged());
        self.set(ResponseFlags::DRAG_STOPPED, resp.drag_stopped());
        self.set(
            ResponseFlags::IS_POINTER_BUTTON_DOWN_ON,
            resp.is_pointer_button_down_on(),
        );
        self.set(ResponseFlags::CHANGED, resp.changed());
        self.set(ResponseFlags::SHOULD_CLOSE, resp.should_close());
        self.set(ResponseFlags::IS_TOOLTIP_OPEN, resp.is_tooltip_open());
    }
    pub fn match_response_any(&self, resp: &egui::response::Response) -> bool {
        if self.contains(ResponseFlags::PRIMARY_CLICKED) && resp.clicked() {
            return true;
        }
        if self.contains(ResponseFlags::SECONDARY_CLICKED) && resp.secondary_clicked() {
            return true;
        }
        if self.contains(ResponseFlags::LONG_TOUCHED) && resp.long_touched() {
            return true;
        }
        if self.contains(ResponseFlags::MIDDLE_CLICKED) && resp.middle_clicked() {
            return true;
        }
        if self.contains(ResponseFlags::DOUBLE_CLICKED) && resp.double_clicked() {
            return true;
        }
        if self.contains(ResponseFlags::TRIPLE_CLICKED) && resp.triple_clicked() {
            return true;
        }
        if self.contains(ResponseFlags::CLICKED_ELSEWHERE) && resp.clicked_elsewhere() {
            return true;
        }
        if self.contains(ResponseFlags::ENABLED) && resp.enabled() {
            return true;
        }
        if self.contains(ResponseFlags::HOVERED) && resp.hovered() {
            return true;
        }
        if self.contains(ResponseFlags::CONTAINS_POINTER) && resp.contains_pointer() {
            return true;
        }
        if self.contains(ResponseFlags::HIGHLIGHTED) && resp.highlighted() {
            return true;
        }
        if self.contains(ResponseFlags::HAS_FOCUS) && resp.has_focus() {
            return true;
        }
        if self.contains(ResponseFlags::GAINED_FOCUS) && resp.gained_focus() {
            return true;
        }
        if self.contains(ResponseFlags::LOST_FOCUS) && resp.lost_focus() {
            return true;
        }
        if self.contains(ResponseFlags::DRAG_STARTED) && resp.drag_started() {
            return true;
        }
        if self.contains(ResponseFlags::DRAGGED) && resp.dragged() {
            return true;
        }
        if self.contains(ResponseFlags::DRAG_STOPPED) && resp.drag_stopped() {
            return true;
        }
        if self.contains(ResponseFlags::IS_POINTER_BUTTON_DOWN_ON)
            && resp.is_pointer_button_down_on()
        {
            return true;
        }
        if self.contains(ResponseFlags::CHANGED) && resp.changed() {
            return true;
        }
        if self.contains(ResponseFlags::SHOULD_CLOSE) && resp.should_close() {
            return true;
        }
        if self.contains(ResponseFlags::IS_TOOLTIP_OPEN) && resp.is_tooltip_open() {
            return true;
        }
        return false;
    }
}
