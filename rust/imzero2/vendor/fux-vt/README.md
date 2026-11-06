# fux-vt

The terminal emulator inside [fux](https://github.com/gold-silver-copper/fux):
a byte parser and a bounded screen with history, stable row identities and
change marks. Every allocation is bounded, every resize is transactional,
and nothing a program writes reaches the host except through options the
host turns on.

The fixed-size cell and much of the inherited behaviour come from Jesse
Luehrs's MIT-licensed [vt100](https://crates.io/crates/vt100) crate, whose
license is kept in `LICENSE`; the parser and grid are fux-vt's own.

```rust
let size = fux_vt::Size::new(24, 80)?; // rows and columns, neither zero
let options = fux_vt::Options::default();
let mut parser = fux_vt::Parser::with_options(size, 8 << 20, options)?; // and bytes of history
// `program`, a `fux_vt::Sink`, sends each reply on to the program.
let mut output: &[u8] = b"\x1b[1mhi\x1b[6n";
while let Some((read, _stop)) = parser.process_until_stop(output, &mut program)? {
    output = &output[read..]; // a frame's start or end, or the work budget
}
let cell = parser.screen().window().row(0).and_then(|row| row.cell(0)).unwrap();
assert_eq!((cell.contents(), cell.attributes().bold()), ("h", true));
```

This document is the contract: what each sequence does, the options, the
limits, and where the standards leave the choice. The references are
ECMA-48, DEC STD 070, the VT520 manual, xterm's ctlseqs, Unicode 17 and the
specs of modern extensions
([`references/README.md`](https://github.com/gold-silver-copper/fux/blob/master/references/README.md)).
Where a standard specifies a behaviour, fux-vt does what it says: ECMA-48
for its control functions, DEC STD 070 for DEC's (over the DEC product
manuals), ECMA-35 for character sets, Unicode for text, the kitty
keyboard, OSC 8 and OSC 133 specs for their protocols, and ctlseqs for
xterm's own functions (over xterm 411 where xterm departs from it). Where
the standards are silent or call a behaviour undefined, it does what xterm
411 does, since fux sets `TERM=xterm-256color`. A row that differs from
xterm says so.

## Sequence matrix

CSI is ESC `[`. A missing or zero count is one unless noted. Sequences
count from one, the API from zero. "Margins" are the scroll region and,
with DECLRMM set, the left and right margins. Tests are in `tests/`
and `src/*/tests.rs`. Every mode SM, RM, DECSET and DECRST set is a
`Mode`, read with `Screen::mode(Mode::X)` as DECRQM reports it;
`Mode::ALL` lists them, and `Mode::number` gives the number each is set
by (`(true, 1049)` for `CSI ? 1049 h`).

| Sequence | Behaviour | Tests |
| --- | --- | --- |
| Text, UTF-8 | Printed in ground state; widths by fux-vt's width rule (below, "Width"); a character split across calls is completed. Invalid UTF-8 prints one U+FFFD, one column wide, per maximal subpart (Unicode 3.9), then rereads the byte that broke it off, and one per byte that starts no sequence (0xc0, 0xc1, 0xf5–0xff, and a continuation byte alone, 0x80–0xbf, which xterm reads as Latin-1); overlong and surrogate forms are invalid. UTF-8-encoded C1 characters are ignored. | `conformance::invalid_utf8_*` |
| Grapheme clusters | A character that continues the grapheme cluster of the cell just printed joins that cell (UAX #29 as written, GB9b's Prepend included, one character at a time, from tables `gen/` makes). A narrow cell whose cluster becomes two columns wide is widened over the cell under the cursor, as a wide glyph printed there would be: under IRM what is there moves right, the second half takes the open link, and a wide glyph it cuts keeps its other half as the pen's blank. In the line's last column it moves as a wide glyph printed there does, with its style and link: with autowrap on to the next line (mode 2027's spec, "Variation Selector 16"), the column it left a spacer; with autowrap off it is not printed (the spec leaves it undefined). Cursor moves and row edits end the cluster; SGR, modes and queries do not. A cluster keeps at most 128 bytes and what its row's text budget allows; the rest is dropped, never split into another cell. `continues_cluster` tells a host what would join, and `clusters` the cells printed text takes. | `unicode::tests`, `ucd::every_grapheme_break_case_prints_a_cell_a_cluster`, `ucd::canonical_equivalents_take_the_same_columns`, `ucd::every_qualified_emoji_is_one_wide_cell`, `ucd::every_variation_sequence_takes_its_style_s_width`, `properties::printed_text_*`, `extended::*cluster*`, `conformance::a_prepend_*`, `conformance::a_cluster_widened_is_placed_as_a_wide_glyph`, `clusters::*` |
| C0 controls | BS: back a column, stopping at the left margin unless already left of it (see CSI ? 45). HT: the next tab stop, else the right margin (the last column without DECLRMM); from right of the margin, or on a line outside the scrolling region, the next stop or the last column (DEC STD 070, HT, note 1); CHT as that many HTs. LF, VT, FF: down a line, scrolling at the bottom margin (between left and right margins only), CR too under LNM. CR: column 0, or the left margin if the cursor is at or right of it, or in origin mode. SO, SI: G1, G0 into GL. SUB: the error character, U+2426, placed as a glyph is, in no character set (DEC STD 070, SUBSTITUTE, p. 5-132). BEL and the rest: nothing visible. BS, HT, LF, VT, FF and CR end a pending wrap (see CSI ? 7). | `semantics::text_controls_*`, `conformance::a_tab_from_*`, `conformance::sub_is_displayed_as_the_error_character` |
| ESC 7 / 8, CSI s / u | DECSC/SCOSC save and DECRC/SCORC restore the position with its pending wrap, origin mode, the pen, protection and the character sets. `CSI s` is DECSLRM while DECLRMM is set. With origin mode restored, the cursor comes back inside all four margins: a line above the top margin goes to it, below the bottom margin to it, and a column left or right of the margins to the nearer (DEC STD 070, DECRC, note 3 and its algorithm). Each screen has its own saved position, pen, protection and character sets, as each of xterm's screens has; the position is clamped by a resize. | `extended::scosc_*`, `conformance::each_screen_saves_*` |
| ESC = / > | DECKPAM, DECKPNM: `Mode::ApplicationKeypad`, off; RIS and DECSTR reset it. Read by `Screen::key_mode`: set, the keypad keys a terminal speaking the kitty protocol names (its codes 57399 to 57416) send ctlseqs' application sequences ("VT220-Style Function Keys": Enter `SS3 M`, 0 to 9 `SS3 p` to `SS3 y`, `SS3 j k l m n o X` for `* + , - . / =`), the modifier after SS3 (`SS3 5 q`), unless Num Lock is held, which overrides the mode (ctlseqs, "PC-Style Function Keys"; Ghostty keeps the mode then); a legacy terminal's keypad arrives as the keys it types. | `modes::decrqm_*`, `keys::encode::tests::the_application_keypad_sends_its_ss3_sequences` |
| ESC ( F, ESC ) F, ESC * F, ESC + F, ESC n / o, ESC N / O | SCS designates G0, G1, G2 or G3: `0` is DEC Special Graphics, `% 5` and `<` (the user-preferred supplemental set) DEC Supplemental Graphics, any other set ctlseqs lists ASCII (DEC STD 070, SCS), and an SCS that names no set changes nothing and is unhandled (DEC STD 070, 3.5.1.3: "Unimplemented functions shall be ignored as if they were not received"). SO, SI, LS2 and LS3 put G1, G0, G2 or G3 in GL; SS2 and SS3 invoke G2 or G3 for the next graphic character alone (ECMA-35, 6.3 and 9), and any other function but SO and SI cancels them: a control, SUB included, an escape or control sequence, DECSC and DECRC among them, or a string (DEC STD 070, 3.8.3.2), but not NUL or DEL, "Ignored on input" (5.9.1); DECSC does not save one (DEC STD 070, DECSC). With Special Graphics in effect, 0x5f–0x7e print a blank and `◆▒␉␌␍␊°±␤␋┘┐┌└┼⎺⎻─⎼⎽├┤┴┬│≤≥π≠£·`. With DEC Supplemental, 0x21–0x7e print Latin-1's 0xa1–0xfe but ¤ at 0x28, Œ at 0x57, Ÿ at 0x5d, œ at 0x77 and ÿ at 0x7d, and U+2426 at the positions the set leaves empty (the VT330 and VT340 manual, Figure 2-5; xterm prints `_` at 0x5f, where the manual has ß). The 96-character sets are unhandled. LS1R, LS2R and LS3R (`ESC ~`, `ESC }`, `ESC |`; ECMA-35, 9.3.2) are taken and change nothing, as no byte reaches GR in UTF-8. RIS and DECSTR designate ASCII into G0 and G1 and DEC Supplemental into G2 and G3, level 2's defaults (DEC STD 070, 3.6.4.2), G0 in GL. GR is reached by no byte in UTF-8 (ECMA-35, 15.4). | `conformance::dec_special_graphics_*`, `conformance::g2_and_g3_*`, `conformance::dec_supplemental_*` |
| ESC SP F, ESC SP G | S7C1T and S8C1T (DEC STD 070, 1.7.2's level 2 list): taken. Replies are 7-bit, as S7C1T asks; S8C1T changes nothing, a departure, as UTF-8 has no C1 bytes (ECMA-35, 15.4). Other `ESC SP` (ANSI's conformance levels) are unhandled. KAM (`CSI 2 h`) and DECARM (`CSI ? 8 h`), on the same list, stay reset for good (see `ModeReports`), departures too: a pane never locks the host's keyboard, and autorepeat is the host's. | `extended::sequences_fux_vt_does_not_implement_reach_the_sink` |
| ESC 6 / 9 | DECBI, DECFI: the cursor a column back or forward; from the left margin back, or the right margin forward, the columns between the margins of each line of the scrolling region shift one the other way, a blank column coming in at that margin, the cursor staying. Outside the margins the cursor moves, ignored at the page's edge. Both end a pending wrap (DEC STD 070, Back Index, Forward Index, whose algorithms shift the columns whatever the cursor's line, where xterm shifts them only with the cursor in the scrolling region). | `margins::decbi_and_decfi_*` |
| ESC D / E | IND: a line feed, LNM aside. NEL: a line feed, then to the column CR goes to. | `conformance::ind_and_nel_*` |
| ESC M | RI: up a line, scrolling down at the top margin (between left and right margins only). Ends a pending wrap. | `margins::index_scrolls_between_the_margins` |
| ESC c | RIS: both screens and history cleared; every mode, the pen, margins, tab stops, protection, links, keyboard stacks, cursor shape and palette back to their defaults. The dynamic and special colours and XTSAVE's saved modes stay. Row identities are never reused. | `semantics::regions_origin_and_reset_*` |
| CSI ! p | DECSTR, as xterm does it: shows the cursor; resets IRM, DECOM, DECCKM, DECKPAM, DECLRMM, reverse wraparound and synchronized output; sets DECAWM to the terminal's setting, on (DEC STD 070, DECSTR: "NVM if present", and revision note 29, recalling it from NVR; the VT520 manual's table says off, and `xterm-256color`'s `is2` and `rs2` rely on it on); resets both screens' margins, the pen, protection, the character sets, the shown screen's saved cursor (home, default attributes and character sets) and the palette. Keeps the screen, cursor, pending wrap, tab stops, alternate screen, LNM, bracketed paste, focus reporting, mouse modes, cursor shape and keyboard state. | `conformance::a_soft_reset_*` |
| CSI Pl ; Pc " p | DECSCL (DEC STD 070, DECSCL): 61 and 62 select level 1 or 2, with a soft reset as DECSTR's (note 3); another level is ignored (note 4). A soft reset at level 1 designates ASCII into all four sets (3.6.4.1), at level 2 DEC Supplemental into G2 and G3 (3.6.4.2); RIS starts level 2 again. Departures: the level gates no function (note 3's "operations ... available in each level"), so a stray `CSI 61 " p` cannot disable half the terminal; Pc's 8-bit C1 replies (0, the default, and 2) are not sent, replies staying 7-bit, as UTF-8 has no C1 bytes (ECMA-35, 15.4). DECRQSS `" p` answers `61;1" p` or `62;1" p`. | `conformance::decscl_*`, `opt_in::setting_reports_*` |
| CSI 4 h / l | IRM, off: a printed glyph pushes what is at and after the cursor right, as ICH does, losing what passes the right margin; the row keeps its soft wrap. | `conformance::insert_mode_*` |
| CSI 20 h / l | LNM, off: LF, VT and FF return the carriage too; IND does not. RIS resets it, DECSTR keeps it. Other ANSI modes are unhandled. | `modes::lnm_*` |
| CSI A B C D E F G H d f \` a e | CUU and CPL stop at the top margin from at or below it, CUD and CNL at the bottom margin from at or above it, else at the screen's edge (xterm's `CursorUp`, `CursorDown`); CNL and CPL go to the column CR goes to. CUF and CUB stop at the right or left margin unless already past it. CUP, HVP, VPA and VPR count lines from the top margin and stay within the margins with DECOM set, and address the whole screen without (so VPR, unlike CUD, passes the bottom margin). CUP, HVP, CHA and HPA count columns from the left margin in origin mode, up to the right one. VPA and VPR keep the column (within the right margin in origin mode). HPR stops at the right margin in origin mode only. All end a pending wrap. | `conformance::line_and_column_addressing_*`, `margins::cursor_movements_*` |
| CSI b | REP: the last printed character, Pn more times, as if printed again; a wide glyph a one-column screen drops is still the last printed. After any control, sequence or string (REP included) it does nothing until a character is printed. After a cluster it repeats the cluster's first character (see "Where the standards leave the choice"), the copies segmented as if printed again: after a flag, two copies of its first regional indicator are a second flag; after a leading jamo, its copies join it in one cell (GB6); copies of a Prepend character join one another and what follows (GB9b); after an emoji VS16 widened, the copies are the narrow emoji alone; after a single shift, the character the shift invoked (ECMA-48 8.3.103: a graphic character "represented by one or more bit combinations"), where xterm prints the byte again in GL. Copies past a screen and history's worth are skipped a line at a time, with the same result. | `conformance::rep_*`, `margins::rep_*`, `clusters::rep_repeats_a_clusters_first_character_as_if_printed_again` |
| ESC H, CSI g, CSI I, CSI Z | Tab stops, one set for both screens: every eight columns at first and after RIS, kept by DECSTR and a resize. HTS sets one at the cursor's column. TBC 0 or none clears that one, 3 every one (DEC STD 070, TBC), and 2 and 5 every one too (ECMA-48 8.3.154, the active line's under TSM's MULTIPLE, kept for good, being every line's), where xterm ignores them; 1 and 4, line tabulation stops, find none. With several parameters TBC acts on each in turn, as DEC STD 070's algorithm (TBC, p. 5-66) does (xterm and Ghostty read the first). CHT is HT n times; CBT goes back n stops, or to the first column (ECMA-48 8.3.7). Both end a pending wrap (see CSI ? 7), CBT moving back from the last column (xterm keeps the wrap); with mode 41 set (`CSI ? 41 h`, ctlseqs, more(1)'s fix) and autowrap, HT and CHT first carry a pending wrap out, a line feed to the first column, the row left unwrapped, as xterm does; reset, HT ends it without carrying it out (DEC STD 070, D.6.1), where xterm keeps it. CTC (`CSI Ps W`, ECMA-48 8.3.17): 0 sets a stop at the cursor, 2 clears it, 4 and 5 clear every one, 1, 3 and 6 (line stops) do nothing, each parameter in turn. TSR (`CSI Pn SP d`, 8.3.156) clears the stop at column n, nothing with no parameter. DECST8C (`CSI ? 5 W`, ctlseqs) sets one every eight columns again. xterm 411 does none of CTC, TSR and DECST8C. | `conformance::tab_stops_*`, `conformance::tbc_acts_on_each_of_its_parameters`, `conformance::hpb_vpb_ctc_tsr_and_decst8c`, `conformance::mode_41_*` |
| CSI Pn j, CSI Pn k, CSI Ps ; Ps ; Ps ; Ps ; Ps T | HPB (ECMA-48 8.3.58) moves the cursor back n columns as CUB does, VPB (8.3.159) up n lines, in origin mode no further than the top margin, as VPR goes down (DEC STD 070, DECOM: "the Active Position cannot be moved above the Top Margin"), each ending a pending wrap (DEC STD 070, D.6.1); xterm ignores both. `CSI T` with five parameters is xterm's mouse highlight tracking (ctlseqs, XTHIMOUSE): ignored, not SD. | `conformance::hpb_vpb_ctc_tsr_and_decst8c` |
| CSI @ P X | ICH, DCH, ECH, within the row. ICH and DCH stop at the right margin; outside the left and right margins they do nothing and keep a pending wrap, otherwise they end it. A wide glyph an edit cuts is blanked whole. DCH ends the row's soft wrap; ICH keeps it. ECH leaves guarded glyphs (SPA's). Blanks take the pen's colours and no other attribute (`bce`). | `margins::edits_stop_*` |
| CSI L M S T | IL, DL, SU, SD within the margins, counts clamped to the region. With left and right margins only the cells between them move, rows keeping their identity, soft wrap and prompt mark, and a wide glyph across a margin is blanked first. IL and DL do nothing with the cursor outside the margins, else move it to the left margin. A soft wrap stays with its row, and the row above the region keeps its own, as xterm keeps them. Rows scrolled off the top of the whole screen (no left and right margins) by SU, a line feed or a wrap go to history; other scrolls discard them. New lines take the pen's colours. | `margins::scrolling_is_bounded_*`, `extended::deleting_or_inserting_lines_*` |
| CSI J / K, CSI ? J / K | ED, EL, DECSED, DECSEL: 0 or none from the cursor, 1 to it, 2 all, each parameter in turn as DEC STD 070's algorithms do (`1;0` is 2; xterm reads the first alone); unhandled if none is known. ED and EL's erased cells take the pen's colours (`bce`); DECSED and DECSEL make each glyph a space in its own rendition and attributes, empty cells staying empty (DEC STD 070, DECSEL and DECSED, as DECSERA): the Space character, which copy, search, reflow and history read as they read a space printed, where xterm gives them the pen's colours (`2x4 '\e[41mab\e[mc\e[42m\e[1;2H\e[?1K'`). Erasing through the last column ends the row's soft wrap. Erasing a row whole leaves the row above its soft wrap, as xterm 411 and Ghostty keep it (`ClearRight`, `ScrnClearLines`), but ends its line there (`Row::continues`, false until a glyph wraps off that row again; history's newest row's, above the top row): a line editor that erases the row its prompt wrapped onto to draw it again there (bash after a resize) starts a line of its own, which reflow reads so, and a host joining rows into lines (fux's answers and copy) reads `continues` (`semantics::a_row_erased_whole_ends_the_line_above`); ED removes the prompt mark of each row it erases whole but the cursor's, whose mark stays even when ED erases all of it, as EL leaves it (a shell redrawing its prompt erases from it). DECSED and DECSEL leave the glyphs DECSCA protects, ED and EL the guarded ones (see CSI " q). All end a pending wrap. | `conformance::blanks_brought_in_*`, `conformance::ed_and_el_carry_out_each_parameter`, `protection::selective_erase_leaves_each_position_its_rendition` |
| CSI r | DECSTBM: top defaults to 1, bottom to the last line. A bottom past the screen is the last line, as xterm takes it; with top above bottom they are set and the cursor goes home, obeying DECOM, otherwise nothing changes (DEC STD 070, 5-25, note 2). Lines scrolled off a region whose top is the screen's go to history, as xterm's do. | `conformance::decstbm_*` |
| CSI ? 6 h / l | DECOM, off. Setting or resetting homes the cursor (to the top and left margins when set). | `conformance::line_and_column_addressing_obeys_origin_mode` |
| CSI ? 7 h / l | DECAWM, on. A glyph in the last column (the right margin's, unless the cursor is past it) leaves the cursor there with a wrap pending (`Screen::pending_wrap`). The next glyph first goes to the next line's start (the left margin) and marks the row soft-wrapped; so does a glyph too wide for the rest of the line, except where the line feed cannot move the cursor. Printing, BS, HT, CHT, LF, VT, FF, CR, RI, cursor moves, ED, EL, ECH, ICH, DCH, IL, DL, DECSTBM, DECOM, resetting DECAWM and RIS end the wrap (DEC STD 070, Appendix D.6.1; xterm keeps it through HT and CHT); SU, SD, SGR, modes and queries keep it. Off, a glyph overwrites the last cell and sets no flag (DEC STD 070, Appendix D.6.1, which lists resetting DECAWM too; xterm keeps the flag in both), and a mark still joins it; a flag DECRC restores is the Last Column Flag all the same, which setting DECAWM again (or DECSTR) keeps, so the next glyph wraps, as the appendix has it; a wide glyph that no longer fits on the row is not printed, and the glyph before it stays, as xterm does. | `conformance::a_pending_wrap_*`, `conformance::the_last_column_flag_*`, `conformance::a_glyph_that_wraps_*`, `conformance::a_wide_glyph_with_no_room_*` |
| CSI ? 1 / 25 / 2004 h / l | DECCKM (off), DECTCEM (on), bracketed paste (off). DECCKM is read by `Screen::key_mode`; bracketed paste is the host's to pass to `encode::paste`; DECTCEM is state only. | `semantics::alternate_mouse_modes_saved_cursor_and_replies` |
| CSI ? Pm s / r | XTSAVE saves each listed mode fux-vt keeps (1, 4, 5, 6, 7, 9, 25, 45, 47, 66, 67, 69, 1000, 1001, 1002, 1003, 1004, 1005, 1006, 1007, 1015, 1045, 1047, 1049, 2004, 2026; 2031 and 2048 with their options); XTRESTORE sets it as DECSET or DECRST would, reset if never saved. RIS and DECSTR keep what was saved. | `modes::xtsave_*` |
| CSI ? 45 / 1045 h / l | Reverse wraparound, off; RIS and DECSTR reset it. With DECAWM on, BS and CUB at the line's first column go on from the last column of the line above, which takes one of the count: for 45 a soft-wrapped row above, for 1045 any row above, or the bottom margin from the top margin (1045 wins). A pending wrap counts as one column past the last. The cursor stops at the first row. | `modes::reverse_wraparound_*` |
| CSI ? 69 h / l, CSI Pl ; Pr s | DECLRMM, off. Set, DECSLRM sets the left and right margins (Pl defaults to 1, Pr to the last column; a right margin past the screen is the last column, as xterm takes it) if left is left of right (DEC STD 070, DECSLRM, note 2), and homes the cursor, obeying DECOM; otherwise nothing changes. Resetting DECLRMM restores the screen's edges, and `CSI s` is SCOSC again. They bound what DEC STD 070 (5.4.3) lists, as each row here says. DECALN, DECSTR, RIS and a resize reset them. A wrap pending where the right margin was is carried out from there. | `margins::*` |
| CSI Pn SP @ / A | SL, SR: the columns between the left and right margins of each line of the scrolling region move Pn left or right (ECMA-48 8.3.121, 8.3.135), blanks in the pen's colours coming in; nothing if the cursor is outside the margins, as xterm bounds them. The cursor and a pending wrap stay. | `margins::sl_and_sr_*` |
| CSI Pn ' } / ~ | DECIC, DECDC: Pn columns inserted or deleted at the cursor's column in each line of the scroll region, between the left and right margins, blanks in the pen's colours; nothing if the cursor is outside them. The cursor and a pending wrap stay; DECDC ends each line's soft wrap. | `margins::decic_and_decdc_*` |
| CSI Ps " q, ESC V / W | DECSCA 1 protects the glyphs printed after it from DECSED and DECSEL (DEC STD 070, 5.11.1.2), 0, 2 or none stops; SPA and EPA start and end a guarded area, whose glyphs ED, EL and ECH leave (ECMA-48 8.3.140, 8.3.49, with ERM reset). Each glyph keeps the protection it was printed with, either, both or none, and each erase spares its own kind only (xterm keeps one kind for the whole terminal, the last DECSCA's or SPA's). Protection is the glyph's: it moves with it through edits, scrolling and history, and REP repeats it; a glyph printed over it and every blank are unprotected. An erase that finds none leaves the protection as it is (DEC STD 070, 5.11.1.2; ECMA-48 8.3.140), where xterm ends it after an ED of the whole screen. DECSC saves DECSCA's attribute; DECSTR ends both for the glyphs printed next, and RIS. `CellRef` does not show it. | `protection::*` |
| CSI ? 4 / 5 / 8 / 60 / 66 / 67 h / l | Kept for DECRQM and XTSAVE. DECSCLM (4), DECSCNM (5), DECBKM (67): off, reset by RIS, kept by DECSTR. DECSCLM is state only; DECSCNM (reverse video) is the host's to draw, read with `Screen::mode` (fux turns each cell's inverse over). DECBKM is read by `Screen::key_mode`: off, Backspace sends DEL, as `xterm-256color`'s `kbs` says, and BS with Ctrl; set, BS, and DEL with Ctrl (ctlseqs; xterm's backarrow toggle). Under the kitty protocol an unmodified Backspace keeps these legacy bytes (its spec, "Disambiguate escape codes"), and its codes stay kitty's. DECNKM (66) is the keypad mode of ESC = and ESC >. DECARM (8) is reported permanently reset; so is DECHCCM (60), as DEC STD 070's VT420 table has it (DECRQM, p. 5-194: "always Reset ... can not be changed"). | `modes::decrqm_*` |
| CSI ? 1004 h / l | `Mode::FocusReporting`, off; RIS resets it. Read by `Screen::focus_report`; DECRQM reports it. | `semantics::focus_and_cursor_shape`, `modes::decrqm_*` |
| CSI ? 2026 h / l | `Mode::SynchronizedOutput`, off: the program is drawing a frame. Holding the display is the host's: `Parser::process_until_stop` stops right after a sequence that sets the mode while it is reset (`Stop::Begun`; set again while set, the frame goes on, with no stop) or resets it while set (`Stop::Ended`: DECRST naming it among other modes, XTRESTORE, RIS, DECSTR), so a host that reads output as it comes knows when a frame is whole; one that holds a frame's bytes unread finds where the parser will end it with `Parser::frame_scan`, carrying nothing out (`FrameScan::end`). A resize taken, or to the same size, ends it too; one refused changes nothing. | `opt_in::synchronized_output_*`, `opt_in::process_until_stop_*`, `parser::tests::a_frame_scan_*` |
| CSI ? 2048 h / l | With `Feature::InBandResize` only: `Mode::InBandResize`, off. Each set reports `CSI 48 ; rows ; cols ; 0 ; 0 t`; `Parser::resize_report` gives the report after a resize. RIS ends it. | `opt_in::in_band_resize_*` |
| CSI Ps SP q | DECSCUSR: `cursor_shape()` is the `CursorShape` Ps names in ctlseqs: 0 (the default) and 1 a blinking block, 0 kept apart as `Zero`, which many terminals take for their own shape, so a host can pass it on as 0 (DECRQSS answers it 1, as xterm), 2 to 6 the steady block, the underlines and the bars, 7 `Initial`, xterm's initial resources, the terminal's own shape as before any is set. Another Ps is ignored. RIS resets it to `Initial`. Mode 12 (`CSI ? 12 h`, AT&T 610, ctlseqs) is its blink, one state with it as xterm 411 keeps them: set, the shape blinks in its form (`Initial` as a blinking block), reset, it is steady (`Initial` stays), and DECRQM reports whether it blinks (`Initial` reset). State only. | `semantics::focus_and_cursor_shape`, `conformance::decscusr_*`, `modes::mode_12_is_the_cursor_shapes_blink` |
| CSI ? 47 h / l | Switches to or from the alternate screen without clearing it. The cursor, pending wrap, origin mode and margins carry across; each screen keeps what DECSC saved on it. The alternate screen has no history, and no cells until first shown. | `conformance::switching_screens_*` |
| CSI ? 1049 h / l | Set: DECSC, switch, clear the alternate screen in the pen's colours (ending a pending wrap), cursor unmoved. Reset: switch back, DECRC. | `semantics::alternate_mouse_modes_saved_cursor_and_replies` |
| CSI ? 1047 / 1048 h / l | 1047: 47, clearing the alternate screen on leaving it. 1048: DECSC when set, DECRC when reset. | `conformance::modes_1047_and_1048` |
| CSI ? 9 / 1000 / 1001 / 1002 / 1003 h / l | `mouse_protocol_mode()`; the latest set wins, and a reset clears only the mode in force. Read by `Screen::encode_mouse`. 1001, highlight tracking, is reported as 1000 is (ctlseqs, "Highlight tracking": on a press "the same information as for normal tracking is generated"); the highlight it then asks of the program (`CSI func ; x ; y ; first ; last T`) and the release it reports (`CSI t`, `CSI T`) are the host's selection's, which fux-vt does not drive: a boundary. | `semantics::alternate_mouse_modes_saved_cursor_and_replies`, `mouse_modes::*` |
| CSI ? 1005 / 1006 / 1015 h / l | `mouse_protocol_encoding()`, by the same rules, as xterm 411's `extend_coords`: 1006 then 1015 is urxvt. Read by `Screen::encode_mouse`. 1016 (SGR with pixels) is permanently reset and changes nothing: a `MouseEvent` names a cell, not the pixel within it. | `semantics::alternate_mouse_modes_saved_cursor_and_replies`, `mouse_modes::each_mode_is_kept_and_reported` |
| CSI ? 1007 h / l | Alternate scroll (ctlseqs; xterm's `alternateScroll`), off: with no mouse reporting asked for, on the alternate screen, `Screen::encode_mouse` takes a wheel turn as cursor up or down through the negotiated key encoder, `lines` times (the host's checked `ScrollLines`, 0–32,767), half the screen's rows with Ctrl, as xterm 411's `AlternateScroll` sends them. | `mouse_modes::alternate_scroll_sends_the_cursor_keys_on_the_alternate_screen` |
| CSI m | SGR. 0 reset; 1 bold, 2 dim, both possible (see "Where the standards leave the choice"); 3 italic; 4 underline, 21 double, `4:0`–`4:5` none, single, double, curly, dotted, dashed (`4:` is `4:0`; larger numbers change nothing); 5 slow, 6 rapid blink, each replacing the other; 7 inverse; 8 hidden; 9 strikeout; 22–29 end them (22 bold and dim, 24 any underline). 30–37, 90–97, 40–47, 100–107 indexed colours; 39, 49, 59 defaults. 38, 48, 58 (foreground, background, underline colour): `5;n`, `2;r;g;b` (values the list lacks are 0; another kind takes only itself), `5:n`, `2:r:g:b`, `2:cs:r:g:b` (colour space ignored). An out-of-range colour is skipped; the rest applies. | `conformance::sgr_*`, `conformance::underline_styles_*` |
| CSI 5 n / 6 n / c | DSR 5 → `CSI 0 n`. DSR 6 → `CSI row ; col R`: the line counted from the top margin with DECOM set (the first line if above it), the column from the left margin in origin mode, a cursor waiting to wrap at the last column, where it is (ECMA-48 8.3.14, DEC STD 070 CPR: the active position; the Last Column Flag is a flag, D.6.1). DA (`CSI c`, `CSI 0 c`) → `CSI ? 1 ; 2 c` (`CSI ? 62 ; 6 ; 21 ; 22 ; 28 c` with an identity: level 2, with selective erase, horizontal scrolling, colour text and rectangular editing, DEC STD 070, DA, Table 1). Other parameters and private forms are unhandled unless an option answers them. | `conformance::cursor_reports_*` |
| CSI Ps x | DECREQTPARM, answered by the VT100 that DA claims without an identity (VT100 User Guide, chapter 3): Ps 0 or none → `CSI 2 ; 1 ; 1 ; 128 ; 128 ; 1 ; 0 x`, 1 → the same with 3, xterm's fixed parameters (ctlseqs). Another Ps, or with an identity, unhandled, as xterm's VT220 leaves it. | `conformance::decreqtparm_*` |
| ESC # 8 | DECALN: the screen filled with `E` in default attributes; all four margins reset (DECLRMM stays), origin mode off, cursor home, no wrap pending; the pen keeps its colours and loses its attributes. Each row's soft wrap and prompt mark end, and every row is single width; a row already all plain `E` keeps its version. | `modes::decaln_*` |
| ESC # 3 / 4 / 5 / 6 | DECDHL top and bottom, DECSWL, DECDWL (DEC STD 070, 5.6.1): the cursor's line takes the rendition, `Row::rendition`, and a pending wrap ends (Appendix D.6.1). A line that is not single width is half as long, half the columns rounded down (5.14.1): its cells past that are lost as it is set, the cursor goes back to its last column, as it does when SU, SD or a switch to the other screen brings such a line under it, and printing, CUP, CUF, CHA, HPA, HT, IND, LF, CUU, CUD and DECRC stop there; ICH and DCH move cells up to it, and DECFRA, DECERA, DECSERA, DECCRA's destination, DECCARA, DECRARA and DECRQCRA's sum stop at each row's end (DECDWL, note 2). A wide glyph that such a line is too short for, where it starts or after a wrap onto it, is not printed, as one wider than the screen is not; a mark at a row's start joins the last cell of the halved line above. DECSWL keeps every cell. DECDWL and DECDHL do nothing while DECLRMM is set, and setting DECLRMM makes every line single width (DECLRMM, 5-29). ED makes a line it erases whole single width (note 1), EL, DECSED, DECSEL and DECSTR leave it, DECALN and RIS make every line single width. The rendition goes with its row through scrolls, history and a resize, which loses what is past the row's end at the new width; reflow keeps such a row a line of its own, never joined or split. Other `ESC #` are unhandled. | `renditions::*` |
| ESC Z | DECID: answered as DA. | `modes::decid_*` |
| CSI … $ x / z / { / v | The rectangular area operations (DEC STD 070, 5.12.1): DECFRA fills with Pch (32–126 through GL's set, so Special Graphics too; 160–255 Latin-1; others ignored, where xterm takes any character) in the pen, as printing writes it; DECERA makes each character a space in the pen's colours (`bce`), its attributes cleared, unoccupied positions staying so; DECSERA makes each glyph DECSCA does not protect a space in its own attributes, SPA's guarded ones too, empty cells staying empty; DECCRA copies text and attributes, read whole first, clipped at the screen. DECCARA (`$ r`) sets the renditions its parameters after the rectangle name (0 all off, 1, 4, 5, 7, 22, 24, 25, 27, and the colours 30–37, 39, 40–47, 49 of the Color Text Extension), in turn, none or none valid being 0; DECRARA (`$ t`) reverses them (0 all four, 1, 4, 5, 7), none valid ignoring it; neither changes a character, the cursor or the pen. DECSACE (`* x`) selects what they change: 0 and 1 (the default) the stream from the first corner to the second, unoccupied positions left alone; 2 the rectangle, its unoccupied positions made spaces and changed too (DEC STD 070, DECSACE); another Ps is ignored. Rectangles are `Pt ; Pl ; Pb ; Pr` from 1, defaults the whole screen, clamped to it, and in origin mode counted from the top and left margins and still clamped to the page, not to them (DEC STD 070, 5.12.1: "affected by the setting of Origin Mode. This control is not otherwise affected by the margins"), DECCRA's destination too and DECRQCRA's rectangle; one inverted is ignored; pages are ignored. The cursor, a pending wrap and each row's soft wrap stay; a wide glyph DECFRA, DECERA, DECSERA or DECCRA cuts (DECCRA's at the source's edges too) is blanked whole; filled and copied cells lose their links. DA names them, rectangular editing (28), with an identity (see CSI c). | `rectangles::*` |
| CSI Pi ; Pp ; Pt ; Pl ; Pb ; Pr * y | DECRQCRA, with `Feature::RectangleChecksums`; otherwise unhandled. A rectangle inverted is ignored, with no reply (DEC STD 070, DECRQCRA). The page is ignored, 0 too, as xterm reads it: DEC's "checksum for all page memory" for page 0 is not taken, a departure, as esctest reads every cell it checks with page 0 and a rectangle. | `opt_in::rectangle_checksums_*` |
| OSC, DCS, APC, PM, SOS | Consumed. OSC ends at BEL or ST (ESC `\`), the others at ST; 0x9c is a byte of a UTF-8 character, not ST. CAN and SUB cancel, and SUB then shows its error character. An OSC's payload leaves out the controls a command string cannot hold (ECMA-48 8.3.89 and 5.6 allow BS to CR) and DEL, as Williams's DEC parser model does (xterm 411 shows each as `?`: `1x4 '\e]2;a\x01b\x07'`). Payloads are kept only for an option: OSC with `Events`, `Hyperlinks` or `Palette`; an OSC's first 64 bytes with `PromptMarks` alone; a DECRQSS request's first 4 bytes with `SettingReports`, C0 controls inside it ignored (DEC STD 070, 3.5.4.4). | `conformance::a_control_string_*` |
| DCS P1 ; P2 ; P3 q … ST | With `Feature::Pictures` only; otherwise ignored, as every DCS fux-vt does not answer. Sixel (DEC STD 070, the VT340's; ctlseqs, "Sixel Graphics"): decoded as it comes into a `sixel::Picture` of RGBA pixels, one kept for each position of the sixel grid and shown as square pixels: a position as tall, to its width, as P1's macro says (2:1 by default; DEC STD 070, 7.1) or the raster attributes' `" Pan ; Pad` (before any sixel, 8.1.2) shows as that many rows, or columns. Sixels past what a picture may keep (4096 positions a side, 4096 by 2160 in all, and the pictures' budget) are not painted, and the picture keeps what fits (11.2.2: sixels past the right margin "are not printed"); it shows at most 4096 pixels a side. Pixels no sixel sets stay clear for P2 1, and take colour register 0, the background, for P2 0 and 2 (7.2), over the picture and the raster attributes' extent, which sixels may draw past; an extent past what a picture may hold is cut to it, as it limits nothing (DEC STD 070, 8.1.2). P3, the grid size, is the screen's one grid, its pixel (7.3). Until a `#` selects a colour, sixels are drawn in colour register 3, the VT340's green, as xterm 411 does (DEC STD 070-9, 10: "Sixel color: Contrasting to background"), not register 0, the background's. The picture is placed at the cursor, a row of cells at a time down from it, scrolling at the bottom and clipped at the right margin (DECSLRM), or the right edge past it (DEC STD 070-9, 11.2.2), and at a double-width line's end, which holds half the columns (DEC STD 070, 5.6.1; a picture so cut is not kept), the clip kept when the screen widens (with DECSDM, `CSI ? 80 h`, set: at the top left instead, clipped at the bottom, scrolling nothing and the cursor kept, as xterm 411 does; ctlseqs), each cell filled with `PLACEHOLDER`, not printed: no pen, insert mode or cluster reaches it (DEC STD 070-9, 11.1); the cursor ends below the picture when a final graphics new line (`-`) took the last band's motion there (DEC STD 070-9, 11.3: the vertical motion executed), as encoders end, but at most 8 rows below its last row (a new line goes down as many rows as the aspect ratio makes six pixel rows, so a few bytes could ask for any number of line feeds, B05), and otherwise on its last row, as xterm puts it, in its first column. Text or an erase over a picture, or its rows leaving history, lets it go. A picture placed over another laid out in the same cell size covers the pixels its opaque ones lie on, which read clear from then on, whatever later hides the newer one: a sixel paints over the pixels below, P2 1 leaving those it doesn't set "unchanged" (DEC STD 070-9, 7.2), as xterm 411 draws pictures in order, a clear pixel showing those below; one covered whole goes, as does one with no room in the budget to note what is covered (a bit a pixel, for each row of it covered), or past the 4,000,000 words of 64 pixels one placement covers (`COVER_WORDS`, B46), the newest covered first; pictures in other cell sizes are never shown together, and cover nothing of each other. A cell copied or moved (DECCRA, ICH, DCH, DECIC, DECDC, and a scroll inside left and right margins) carries its character and attributes, and a picture's cell has neither: it lands as a blank, so no copy makes another picture's cells or gives back a picture that text took. Scrolling carries a picture; one placed between left and right margins (DECLRMM), where a line feed on the bottom margin scrolls only their cells, is on the rows its cells went to as it was placed, the final graphics new lines' scrolls included, and a picture such a scroll lifts past the region's top, or a line feed outside those margins stacks on one row, has lost bands and is not kept; what scrolls off a screen that keeps no history, or off a region's top margin, is cropped from its top (`Shown::cropped`), the rest shown (DEC STD 070-9, 11.2.4); the two screens keep 64 at most together, those no longer shown let go first, and the pictures of both screens and the one being drawn, as it grows, hold `PICTURE_BYTES` (96 MiB, `Parser::set_picture_bytes`) of pixels at most, those no longer shown let go first, then the oldest; RIS keeps none. Inside it SUB is a blank sixel, CAN ends the picture unplaced, BEL rings and every other C0 control is ignored (DEC STD 070's sixel chapter, 9.1); a GR byte (0xA0 to 0xFF) is read with its 8th bit cleared (9.4), each byte of a UTF-8 character too (`é`, C3 A9, is the sixel `C`, then `)`, which ends a command and does nothing; see "Sixel data is read a byte at a time" in "Where the standards leave the choice"), and a C1 byte, no control in UTF-8, is ignored as a C0 control is, where 9.3 has it end sixel mode. The cell size is the host's (`Parser::set_host_graphics`, answering `CSI 14 t` and `CSI 16 t`), at least 2 by 6 (`CellSize::LEAST`; `CellSize` refuses a smaller one); without one a picture has no cells to be laid out in and none is kept (nor while the host draws no sixel: such a picture is read only to tell whether it drew anything; `Sink::picture(false)` tells, as each ends, each picture not kept, each one kept and not placed: ended once the host had no cell size, lifted past a region's top, or cut by a double-width line's end, and each one cut, placed or not: sixels or an extent past what a picture may keep, or pixels the budget refused; not one that drew nothing, or that CAN ended, nor one placed whole and let go later, as history let go is not), `CSI 14 t` and `CSI 16 t` go unanswered, and DA1 says no sixel; DA1 says `4` and XTSMGRAPHICS (`CSI ? Pi ; Pa ; Pv S`) answers as xterm 411 does, with its statuses: read, read the maximum or reset give 256 colour registers and the screen's pixels while the host shows sixel, no more than the decoder keeps whole (4096 a side, 4096 by 2160 in all; ctlseqs: "the minimum of the window size and the graphic size"); setting, ReGIS and any request without sixel shown fail (3); an unknown item is 1, an unknown action 2. Read with `Screen::pictures`; `sixel::encode` writes one back. | `extended::*sixel*`, `sixel::tests` |
| OSC 133 | With `Feature::PromptMarks` only. `A`, `N`: a fresh line (CR and IND unless in the first column), then the row is marked as a prompt's start; `N` first ends the command running (`Screen::commands`), as a `D` with no status. `P` with `k=i` or no `k`: the row marked, no fresh line. `L`: the fresh line alone. `C` marks where a command's output starts, at the cursor (past the last column while a wrap is pending), and `D ; status` where it ends and how (`Screen::commands`; `Commands::started` counts the commands begun, a `C` while one runs beginning none; with `Events`, each command ended is a `CommandEnded` event). Every mark counts, whichever program sent it. The rest change nothing. The mark moves with its row through scrolling, history and IL/DL, and through a reflow onto the row its first cell goes to; ED erasing the row whole, DECALN and RIS remove it; EL keeps it. A row marked or unmarked takes a new version; one marked as it was keeps its own. | `prompts::*` |
| Anything else | Ignored; C0 controls inside a CSI still execute. A complete CSI or escape sequence fux-vt does not implement goes to `Sink::unhandled`. Unknown DEC private modes, and sequences cut short by the parser's bounds, are dropped unreported. | `semantics::ignored_sequences_*`, `extended::sequences_*` |

## Opt-in outputs

With `Options::default()` everything below is off, and the behaviour is
the table above. `Parser::with_options` turns each on independently: a
`Feature` each (`Options::new().with(Feature::Reflow)`), and `identity`
by `Options::with_identity`. `Parser::process_until_stop` delivers to a
`Sink`, whose `reply`, `event`, `unhandled` and `picture` methods discard
by default; `()` is the sink that discards them all.

| Feature | Behaviour | Tests |
| --- | --- | --- |
| `Events` | OSC 0 → `IconName`, `Title`; OSC 1 → `IconName`; OSC 2 → `Title`, the last title kept as `Screen::title`; OSC 52 `Pc;Pd` → `Clipboard { selection, data }` unless `Pd` is `?`; OSC 10–19 → `ColorQuery { number, bel }` for each `?`, each parameter being the next colour; BEL in ground, escape or CSI state → `Bell`; with `PromptMarks`, OSC 133 `D`, or `N`, ending a command that ran → `CommandEnded(status)`. Payloads are raw bytes (`Kept`). An OSC over `OSC_PAYLOAD_LIMIT` (64 KiB) gives the event its start makes, `cut` set, and nothing past the limit is buffered; the palette's, OSC 10–19's and a link's (OSC 8) are none. | `opt_in::events_*`, `opt_in::osc_payloads_*` |
| `ExtendedReplies` | DECXCPR `CSI ? 6 n` → `CSI ? row ; col R`, as DSR 6; DA2 `CSI > c` → `CSI > 1 ; 10 ; 0 c`; DECRQM as `ModeReports`. The DSRs of DEC's other devices, which fux-vt has none of (DEC STD 070, DSR, and its extensions): `?15` → `?13` (no printer), `?25` → `?23` (no UDK extension, where xterm says unlocked), `?26` → `?27;0` (keyboard language unknown, as a VT220 reports it), `?55` → `?53` (no locator), `?56` → `?57;0`, `?62` → `CSI 0 * {` (no macro space), `?63;Pid` → `DCS Pid ! ~ 0000 ST` (no macros), `?75` → `?73` (power-up) until reported once after the screen is made or RIS, then `?70` (xterm answers 70 at once), `?85` → `?83` (no sessions). | `opt_in::extended_replies_*` |
| `ModeReports` | DECRQM alone. `CSI ? Ps $ p` → `CSI ? Ps ; Pm $ y`: 1 set or 2 reset for 1, 4, 5, 6, 7, 9, 25, 41, 45, 47, 66, 67, 69, 80, 1000, 1001, 1002, 1003, 1004, 1005, 1006, 1007, 1015, 1045, 1047, 1049, 2004, 2026, 2031 (with `ColorSchemeUpdates`), 2048 (with `InBandResize`); 4 for 8, 60 (DECHCCM, DEC STD 070's VT420 table, DECRQM p. 5-194) and 1016 (no pixel is known); 3 for 2027 (`Mode::GraphemeClustering`, permanently set); 0 otherwise, each a `ModeStatus`. `CSI Ps $ p`: 1 or 2 for IRM (4) and LNM (20); 4 (permanently reset) for GATM, KAM, CRM, SRTM, ERM, VEM, BDSM, DCSM, HEM, PUM, FEAM, FETM, MATM, TTM, SATM and TSM (1 to 3, 5 to 11, 13 to 18), which have the reset state only (ECMA-48 7.1), and EBM (19), the 4th edition's, which the 5th removed (F.5.1) but DEC STD 070's VT420 table answers so (DECRQM, p. 5-194): the keyboard is never locked, controls are performed, ED, EL and ECH spare a guarded area, there is no implicit bidirectional processing and no data component; 3 (permanently set) for SRM (12), GRCM (21) and ZDM (22), which have the set state only: SIMULTANEOUS (ECMA-48 7.2.15; DEC STD 070's SRM, local echo off, the default), as fux-vt never images what is typed; CUMULATIVE (7.2.8), as an SGR changes only the renditions it names; DEFAULT (F.4.2), as a parameter of 0 is its default; else 0. | `opt_in::mode_reports_*`, `modes::grapheme_clustering_*`, `modes::decrqm_*` |
| `InBandResize` | Mode 2048 (see its row). Off: unrecognized, DECRQM 0. | `opt_in::in_band_resize_*` |
| `SizeReports` | `CSI 18 t` → `CSI 8 ; rows ; cols t`. `CSI 14 t` and `CSI 16 t` (pixels) are answered with no feature, from the host's cell size (see the sixel row). | `opt_in::size_reports_*` |
| `ColorSchemeUpdates` | Mode 2031 tracked: `Mode::ColorSchemeUpdates`, DECRQM; RIS ends it. The host sends the reports and answers `CSI ? 996 n`, which stays unhandled. Off: unrecognized, DECRQM 0. | `opt_in::colour_scheme_updates_*` |
| `KittyKeyboard` | A kitty keyboard flag stack per screen: `CSI > Ps u` pushes (a full stack of 32 drops its oldest), `CSI < Ps u` pops Ps (1), `CSI = Ps ; Pm u` sets (Pm 1 replace, 2 add, 3 remove); of a flags number only the spec's bits fux-vt honours are kept, 1, 4, 8 and 16, not report event types (2), as fux sends no repeats or releases, so a program that sets it and asks learns so (the spec's "Detection of support"); `CSI ? u` → `CSI ? flags u`. `CSI > 4 ; Pv m` sets modifyOtherKeys (Pv 0 or none: off; 1 to 3 its levels, 3 sending unmodified keys as `CSI 27 ; 1 ; code ~` too; past 3 no key changes); a subparameter of Pv, xterm's mask of modifiers to factor out (ctlseqs, "Alt and Meta Keys"), is ignored, and so is one of the resource (`CSI > 4 : 1 m`), as xterm 411 ignores it; `CSI > m` with no parameters turns it off, as it resets every resource (ctlseqs, XTMODKEYS), and so does `CSI > 4 n` (disable); other resources are unhandled, `CSI > 0 m` (modifyKeyboard's reset) among them. XTQMODKEYS `CSI ? 4 m` answers `CSI > 4 ; Pv m`, Pv 0 when off (ctlseqs); the other resources, which fux-vt keeps none of, are not answered. RIS clears it all. Read with `Screen::kitty_keyboard_flags` and `Screen::key_mode` (its `other_keys`, a `keys::encode::OtherKeysLevel`, `None` when off); the host encodes keys. Off: all unhandled. | `extended::kitty_*`, `extended::modify_other_keys_*`, `extended::xtqmodkeys_*` |
| `Reflow` | A resize re-wraps the primary screen and its history. Each logical line (soft-wrapped rows and the row ending it) loses its blank tail and is laid out at the new width without splitting wide glyphs: one that does not fit leaves a blank spacer, which later reflows and copies skip. The cursor and saved cursor stay on their characters. Surplus rows go in order: blank lines below the cursor (only as far as the screen's own lines are past the new height, so none is given up to bring a history row back above the cursor), the oldest lines into history, history past its limit; rows still below the screen once the cursor's row is at the top are dropped. A line's k-th row keeps the identity of its k-th row before; every row takes a new version. The margins reset. The alternate screen resizes without reflow. | `extended::*reflow*`, `extended::a_shrink_takes_no_history_back_onto_the_screen`, `properties::reflow_*` |
| `identity` | An `Identity { name, version }`. DA → `CSI ? 62 ; 6 ; 21 ; 22 ; 28 c`; DA2 → `CSI > 1 ; Pv ; 0 c`, Pv = the version's parts, at most three, as digits in base 100: `1.2.3` is 10203, `1.2` is 102, `7` is 7 (a `-` or `+` suffix dropped); XTVERSION `CSI > q` → `DCS > \| name version ST`, unless name and version pass 48 bytes; Without one, XTVERSION is unhandled. | `extended::*identity*` |
| `Hyperlinks` | OSC 8 `params ; URI`: glyphs printed while a link is open take it; an empty URI or RIS closes it. `Row::link` gives a cell's `Hyperlink`: `uri()` and `key()`, equal for the cells of one OSC 8 without an id, or of one id (`id=` in params) and URI. Blank cells have none; a wide glyph's halves share one. Links follow their cells through scrolling, history, edits, resize and reflow. A URI over 2083 bytes, an id over 250, a byte outside printable ASCII, or an OSC over the payload limit opens nothing and closes the open link. Each screen's links hold at most 4 MiB of heap, as allocated: each link's URI and id with its `Arc`'s counts, and the table of the links with an id, two tables while it grows; when full, the links its rows no longer have are let go, then the oldest history rows lose theirs until half is free; the screen's rows keep theirs, and a link that still does not fit is dropped, with no retry for 256 links. A changed link changes the row's version. | `hyperlinks::*` |
| `SettingReports` | DECRQSS (`DCS $ q Pt ST`), as xterm 411 answers it: `DCS 1 $ r Pt ST` for `m` (the pen), ` q` (DECSCUSR), `r` (DECSTBM), `s` (DECSLRM, the screen's edges without DECLRMM) `"q` (DECSCA: 1 while DECSCA 1 protects, else 0, SPA being no DECSCA), `"p` (DECSCL: the level, and 1 for the 7-bit C1 replies fux-vt sends) and `*x` (DECSACE's Ps as set); `DCS 0 $ r ST` for another request or one over 4 bytes. The pen is `0`, then 1, 4, 5, 7, 8, 2, 3, 9, 21, foreground, background (16 colours short, others `38:5:n` or `38:2::r:g:b`), with an underline style as `4:n` in 4's place, rapid blink 6 in 5's and the underline colour last (`58:…`): a curly underline alone is `0;4:3`, which neovim checks for. `Attributes::write_sgr` writes it (`SgrForm::Report`), and the same attributes as a host paints them on a terminal (`SgrForm::Paint`: semicolon colours, the underline colour first, an underline style only if the terminal draws them). The terminal's own cursor shape (`Initial`) is answered `7 SP q`, xterm's initial resources (ctlseqs, DECSCUSR). XTGETTCAP (`DCS + q Pt ST`) is answered `DCS 0 + r ST`, ctlseqs' answer for an invalid request, as fux-vt names no capability: read to its end, nothing kept or echoed. | `opt_in::setting_reports_*`, `opt_in::xtgettcap_is_answered_as_an_invalid_request` |
| `PromptMarks` | OSC 133 (see its row). | `prompts::*` |
| `Palette` | The colours a program sets, as xterm 411 keeps them. OSC 4 sets or queries palette entries (pairs of number and specification; 256–260 are the special colours, OSC 5's 0–4); OSC 10–19 the dynamic colours, each parameter the next; OSC 104, 105, 110–119 reset them. Specifications: `rgb:R/G/B` (1–4 hex digits a channel) and `#RGB` to `#RRRRGGGGBBBB`; XParseColor's device-independent spaces (`rgbi:`, `CIEXYZ:` and the rest) and colour names are not read. Kept at 8 bits a channel; answered `OSC 4 ; n ; rgb:RRRR/GGGG/BBBB`, each byte twice, ended as asked. OSC 4 and 5 stop at the first bad number or specification; OSC 10–19 skip a bad one; OSC 104 and 105 stop at the first non-number; OSC 110–119 with a parameter do nothing; OSC 105 alone resets every special colour (ctlseqs; xterm 411 resets none). Unset entries are answered with the host's colour for them, where it gave one (`Parser::set_host_palette`, entries 0 to 15, which no reset clears and which change nothing drawn), else not at all, as no one knows them (xterm's defaults are no answer for another terminal); unset dynamic colours become `ColorQuery` events with `Events`; unset special colours are not answered. RIS and DECSTR reset the palette only. Read with `Screen::palette_color`, `dynamic_color`, `colors_changed`; the host draws them. | `palette::*`, `palette::an_entry_not_set_is_answered_with_the_hosts_colour` |
| `Pictures` | Sixel (see its row) for a host that draws pictures. Off, a sixel string is read and dropped: nothing is decoded or allocated, its cells hold what the program wrote around it (no `PLACEHOLDER`), the cursor stays, and DA1 and XTSMGRAPHICS say no sixel whatever `Parser::set_host_graphics` says. | `extended::*sixel*`, `screen::tests::pictures_*` |
| `RectangleChecksums` | DECRQCRA → `DCS Pi ! ~ xxxx ST`, xterm 411's default sum, the VT520's, but for the cells below: each cell's character plus 0x08 hidden, 0x10 underline, 0x20 inverse, 0x40 blink, 0x80 bold, summed in 16 bits and negated. A character past Latin-1 or below a space, and a wide glyph's second half, count as ESC; a Special Graphics or DEC Supplemental glyph past Latin-1 as its DEC code, as xterm 411 sums it (─ 0x1D, π 0xC6, Œ 0x97; `unmap_DEC_Spec_Graphic`, `unmap_DEC_Supp_Graphic`); combining marks are added; an empty cell is a space. A departure from xterm 411, which sums each cell's byte as written in the set it was written from: fux-vt sums the character a cell shows, so a character U+0080 to U+00FF counts as its Latin-1 code (xterm: from UTF-8, its low seven bits, é 0x69, ° 0x30, and U+00A0 ESC, ÿ 0); a Special Graphics or Supplemental glyph as its DEC code even when it came as UTF-8 (xterm: ESC); Special Graphics' blank (0x5F) as a space (xterm: nothing); a cell after ICH or DCH as what it now holds (xterm: what was first written there); and an empty cell as a space plus its rendition, as esctest reads it (xterm: nothing for a cell never drawn on or erased to the line's end, a space for one ECH or ICH blanked). `Pp` is ignored. The rectangle is one-based, relative to the top and left margins in origin mode, and clamped to the screen whatever the margins (DEC STD 070, 5.12.1); 0 is its whole extent; an inverted one has no reply (DEC STD 070, DECRQCRA: the sequence is ignored). It lets a program read the screen, so fux leaves it off; esctest needs it. | `opt_in::rectangle_checksums_*` |

## Keys

`fux_vt::keys` is the host's side of input: what a user's terminal sends,
decoded, and what a program asked for, encoded. It needs no option.

- **Decoding:** `keys::decode::Decoder` turns a terminal's raw bytes,
  however split, into `Input`s: keys (legacy and xterm's modifiers, and
  the kitty keyboard protocol with disambiguate and alternate keys),
  bracketed pastes (whole, at most the limit
  `Decoder::with_paste_limit(timing, limit)` sets), focus changes,
  mouse reports (SGR and the default encoding) and answers to the host's
  own questions (DA1, the kitty flags, DECRQM, OSC 10 and 11, OSC 4
  palette entries, the colour scheme, DECRQSS for underline styles). An
  Escape before a whole CSI or SS3 key is that key with Alt, as libtermkey
  reads it (`ESC ESC [ A` is Alt-Up); `ESC ESC` alone is two Escapes, a
  choice, as vim's users type it, where libtermkey reads Alt-Escape. A
  byte that can be no part of a control sequence (ECMA-48 5.4), an ESC
  among them, ends a CSI unfinished: `ESC [` alone is Alt-[, one with
  parameters is dropped, and the byte begins the next key. A
  lone Escape is a key once
  the Escape delay in its `Timing::new(escape_ms, paste_ms)`
  passes without more, which the host learns from
  `Decoder::deadline` and tells with `Decoder::timeout`. A character
  begun, a UTF-8 lead byte without its continuation bytes, after an
  Escape or not, waits up to the fixed one-second reply delay for its bytes,
  at most 4, and a byte that can't
  continue it ends it, one U+FFFD for what came (Unicode 3.9's maximal subparts). Every buffer is
  bounded (THREATS.md B23 to B27): a paste keeps at most its limit and 5
  bytes (65,541 at a 64 KiB limit), however many come in one read; an unfinished sequence at most
  258 bytes, a long read giving it only what decides it; an answer past
  its window (OSC 128, DCS 256, each with the BEL or the ESC that begins
  its end; `CSI ?` 64 and then 256), and a key's CSI past 32 parameter
  bytes, is dropped to its end, none of it typed, however it is split; a
  key's CSI so long is no key, so an Escape before it is an Escape at
  once, and it ends at the first byte no control sequence holds, a final
  byte dropped with it and any other beginning the next key;
  the output is at most one
  `Input` a byte. Each encoded key or mouse report is at most 64 bytes
  (B45; 30 measured over every key, modifier, mode and encoding). An
  alternate-scroll event expands into at most 32,767 arrows in chunks
  of at most 64 bytes, admitted or refused as one action by the host.

**Encoding** reads what the program asked for from its screen:

| Encoder | Sends |
| --- | --- |
| `keys::encode::key_bytes(stroke, screen.key_mode(), out)` | the key as `Screen::key_mode()` asks (DECCKM, DECKPAM, DECBKM, the kitty flags, modifyOtherKeys): kitty protocol; xterm's `CSI 27 ; m ; k ~`; or legacy bytes, Ctrl mapping only what Xlib maps (`Ctrl-;` is `;`), Alt an ESC before them (Alt-Escape is ESC ESC). The legacy bytes and modifyOtherKeys are real xterm 411's (tests/data/keys/xterm-keys.txt, `input_suites::xterm_keys`): at level 2 Shift alone on a character Ctrl makes a control of (`@` to DEL, the capitals among them) or on Space is `CSI 27 ; 2 ; code ~`, the shifted character's code, and on any other (`!`) types its text; level 3 escapes every shifted character, and Ctrl alone on Backspace; Shift-Tab is `CSI Z`, ISO_Left_Tab's, with Ctrl, Alt or both in the legacy mode and at level 1, and alone at level 2, `CSI 27 ; 2 ; 9 ~` at level 3 and `CSI 27 ; m ; 9 ~` at level 2 with Ctrl or Alt; Ctrl-Alt-? is ESC `?`, as Ctrl-? is DEL by xterm's own translation. `Key::F` holds a `Function`, F1 to F35: F13 to F35 are kitty's 57376 to 57398, and send nothing in the legacy modes, where xterm has no bytes for them. It takes any `KeyMode` |
| `keys::encode::paste(text, screen.mode(Mode::BracketedPaste), out)` | the text, framed by `CSI 200 ~` and `CSI 201 ~` with bracketed paste (2004) set; inside the frame every end marker (`ESC [ 201 ~`, or with the C1 CSI U+009B) is removed, as often as removing one makes another; each LF goes as CR, as xterm 411 sends a paste (`button.c`, `pasteLiteralNL` off); other controls go as pasted, where xterm spaces those its tty and `disallowedPasteControls` name (the host's paste has passed its own terminal's filter) |
| `Screen::focus_report(focused)` | `CSI I` or `CSI O` with focus reporting (1004) set, else nothing (`None`) |
| `Screen::encode_mouse(event, lines)` | a `keys::mouse::MouseEvent`, whose `MouseAction` carries its button (a press always names one), as xterm reports it in the program's mode and encoding: X10 (9) presses of buttons 1 to 3 without modifiers; 1000 presses and releases (the wheel's turns as presses, never released); 1002 also motion with a button held; 1003 also motion with none; 1001 as 1000; in the default encoding (a 0 byte for a position past 223, as xterm 411 sends it), UTF-8 (1005, Cb too, a 0 byte past 2015), SGR (1006, any position, a release naming its button) or urxvt (1015, `CSI Cb ; x ; y M`, Cb plus 32, a release unnamed); with alternate scroll (1007), a wheel turn's cursor keys in the negotiated key protocol, with the ordinary distance supplied as checked `ScrollLines` (0–32,767). Returns `Some` of an iterator of chunks of at most 64 bytes, keeping whole keys in order; the host admits all chunks as one action or refuses the whole action. |

`encode_mouse` returns `None` for an event the mode does not report, and
`Some` of an empty iterator for an accepted scroll of zero distance.
`focus_report` returns the report or `None`. Motion within one cell is
for the host to drop.

- **Colours:** `keys::colour` holds `Rgb` (read from and answered as
  `rgb:RRRR/GGGG/BBBB`), `Scheme` (dark or light, as mode 2031 reports
  it) and `Colours`, a terminal's foreground, background and scheme.
- `fux_vt::bytes::ByteQueue` is the byte queue the decoder reads
  from, for hosts that need one.

Tests are in `src/keys/decode.rs`, `src/keys/encode.rs` and
`src/keys/mouse/tests.rs` (each encoder against ctlseqs, every mouse report
decoding back to its event, no paste ending its bracket); `fuzz/`'s `keys`
target feeds the decoder arbitrary bytes and splits, re-encodes each mouse
event and paste it decodes, and checks both. `compare/`'s `encoders` holds
every encoder beside libghostty-vt's, with its recorded verdicts.

## Deliberate boundary

- Programs cannot operate the window: resizing, moving or iconifying it,
  and 132 columns are refused in every mode, and the title stack is the
  host's: `CSI Ps t` other than `CSI 14 t`, `CSI 16 t` and `CSI 18 t` is
  unhandled (fux keeps a title stack); DECCOLM is ignored.
- By default, output causes no title, bell or clipboard side effects, and
  no OSC payload is kept. OSC 52 is only ever an event, which the host
  acts on (fux copies it) or drops.
- Keypad, mouse and focus modes change only what the host's encoders
  send. Keys, pastes, focus
  changes and mouse events are encoded only when the host asks
  (`keys::encode`, `Screen::focus_report`, `Screen::encode_mouse`, see
  [Keys](#keys)); nothing a program writes makes fux-vt send input.
- Sixel is the one graphics protocol (see its row), with
  `Feature::Pictures`: pictures are kept as pixels for the host to draw
  (`Screen::pictures`), within their budget (see [Bounds](#bounds)),
  however large their data says they are; none reaches the host's
  terminal unless the host sends it.
- Only the primary screen has history, up to its limit. CSI 3 J (ED 3,
  erase saved lines) and `Parser::clear_history` drop it, from either
  screen.
- Each compatibility choice is documented here and tested on its own,
  never hidden in a broad allowlist.

## Parser

A direct implementation of Paul Williams's DEC ANSI state machine
(<https://vt100.net/emu/dec_ansi_parser>), without a parser crate. UTF-8
replaces the 8-bit controls: raw C1 bytes are invalid UTF-8, and 0x9c in a
string is a byte of a character, not ST (ECMA-35, 15.4: with a coding
system other than ISO 2022's designated with DOCS, as UTF-8 is, the C1
controls of ECMA-48 are not used; CONFORMANCE.md lists the parser's rows). Parameters saturate at
`u16::MAX`. At most 32 fields are kept (B22): the rest are ignored and the
sequence runs with the 32, as DEC STD 070 (3.5.3.1) lets parameters
"beyond the maximum number supported" be (xterm 411 differs: `1x4
'\e[0;…;0;1;4;7mX'` with 30 zeros is plain there). More than two
intermediates make the sequence ignored. CAN and SUB cancel any
sequence, SUB then showing its error character (DEC STD 070, p. 5-132,
note 2; in a control string too, as xterm does); ESC starts a new one.

The common input skips the byte-at-a-time state machine, each fast path
doing exactly what it would:

- printable ASCII, found eight bytes at a time, is written into the row's
  cells a run at a time (a glyph at a time under IRM or Special Graphics);
- valid non-ASCII UTF-8 is decoded a run at a time;
- an escape sequence, or a CSI of parameters alone, is read in a loop of
  its own;
- an OSC payload is taken up to its terminator in one pass.

On every push, `parser::tests::text_runs_keep_their_printing_context` checks
non-ASCII runs in contexts that change printing, including links and protection;
`properties::*`, `clusters::*` and `conformance::*` check segmentation and sequence
semantics. The `terminal` fuzz target compares whole, chunked and byte-at-a-time
processing nightly and on request.

## Cells

A grid stores a cell in 8 bytes (`compact.rs`):

- 4 bytes of text, one character. A cluster of 5 to 17 bytes goes in the
  row's short text, a longer one (up to 128) in its long text, which holds
  at most 32 bytes a cell and 128 more. Overwritten text stays until the row
  runs out of room and is compacted; a cluster that still does not fit is
  cut to 17 bytes of whole characters.
- A 28-bit style number. Common styles are their own number: default or
  indexed colours with no underline colour and any rendition but rapid
  blink and underlines other than single, or a direct foreground on the default
  background, bold or italic (`style.rs`). Other styles are kept once in a
  table per screen. The table is swept, not counted: once it holds twice
  the styles in use at the last sweep, and at least max(4096, cells / 8),
  unused styles go and cells are renumbered. Printing in the pen looks
  nothing up.
- Bits for the halves of a wide glyph, text kept in the row, and
  protection.

Readers never see a stored cell: `Row::cell` and `Row::cells` return
`CellRef`s, which carry the cluster's text and the style's attributes.
`Attributes::write_sgr` writes the SGR parameters that set attributes
after a reset, and `Attributes::write_sgr_change` the whole sequence that
takes a terminal's pen from one set to another, the shorter of the change
alone and a reset. A
host that stores screen contents uses `Cells` (`Cells::new`,
`Cells::set`), which stores
cells as rows do, with each cell's attributes beside it in place of a
style, so it keeps and cuts text as a row does. A host makes a cell to
store with `CellRef::new` or `CellRef::wide_continuation`. `Cells` holds
no links.

A zero-width character after an empty cell joins it as a space and the
character; in the first column it joins the row above only if that row is
soft-wrapped, and is otherwise dropped. A glyph wider than the grid is
dropped, the cursor unmoved but for a wrap pending before it, which is
carried out, as xterm does.

## Storage, identity and windows

Each screen is a grid: rows of cells in slots, a ring giving each screen
row's slot, and history.

- **Scrolling** the whole screen turns the ring and moves no cells. Each
  row records how far it may differ from blank, so erasing, recycling and
  taking a row into history touch only that far.
- **History** holds at most `history_bytes` (0: none) of memory, counted
  as what it holds: the rows' 24-byte records in chunks of up to 512 rows,
  the blocks of cells as their room (8 bytes a cell), the rows' text, and
  the maps of the rows with text or links, by their room, and the links'
  runs (B15): none for a row with one link, which keeps its run in its
  place in the map, and 32 bytes a run for a row with more (a screen
  row's runs take at most 8,192 bytes at 200 columns). A row
  scrolled
  off keeps its cells up to its last that is not a default blank, packed
  after the rows before it in blocks of 4,096 cells, or one of its own
  past 2,048 (`history.rs`), with its text and links; past them it reads
  blank, at its original width. At its budget, the oldest rows go: a
  chunk's or a block's worth as one is made, so history keeps at most
  that much less than its budget's worth of rows.
- **The alternate screen** has no history, and no cells until first shown;
  RIS and resizes keep an unmade one unmade.
- **Identity.** Every row has a `RowId`, kept through edits and scrolling
  while it is retained, and never reused: 64 bits counted by one do not
  run out. A row's version changes with each edit that
  changes its cells, links, soft wrap or prompt mark, and nothing else: erasing blanks,
  or rewriting a glyph as it was, keeps it; ICH and DCH always change it.
- **Limits.** A `Size` has no zero to refuse: `Size::new` refuses one,
  its `SizeError` saying which.
  A screen of more than 64 Mi (67,108,864) cells is refused
  (`Error::Capacity`; B38); history
  keeps at most 1,048,576 rows and 64 Mi cells, whatever its budget; text
  budgets and link bounds cover the rest.
  Arithmetic is checked. After an error the terminal stays usable: input
  already applied may stay, and a partial scroll still forces a refresh.
- **Resize** builds the new grids first, so a failure changes nothing and
  peak memory can hold both. Tab stops stay; the scroll region and the
  left and right margins reset, as xterm resets them.

### Without `Feature::Reflow`

Resize is not paragraph reflow: rows keep their first cells and lose the
columns past the new width, and at a new width the screen's rows lose
their soft wraps (history keeps them; at the same width they stay). Rows move around the cursor so its line stays
visible: a shrink drops rows below the cursor first, then moves rows above
it into history (or drops them, on the alternate screen); a grow pulls rows
back from history, then adds blank rows below. Both cursors move with
their rows and are clamped, and keep a pending wrap, at any width, as xterm
keeps it.

With `reflow`, each line is laid out a run of cells at a time (as many as
fit before a row ends, a wide glyph moves or a spacer is skipped), in two
passes over the rows with no line gathered in memory, so a resize costs
its rows rather than their cells. At one column, the runs between wide
glyphs are laid out, each wide glyph left out
(`extended::reflow_to_one_column_leaves_wide_glyphs_out`).

### Windows and change marks

A `Row` is a retained row and its place among the rows retained, history
first (`Screen::rows`): it borrows the screen, so a place can't be held
past output that moves it; a `RowId` finds it again later
(`Screen::row_index`). A `Window` is an immutable view of the screen
(`Screen::window`); reading it never changes where output lands.
`Window::iter` walks its rows top to bottom with their numbers, and
`Window::cell` reads a cell at a row and column, both `u16` as the
cursor's position is. A
picture's cells read as blanks (`CellRef::shown`), and
`Row::ends_in_spacer` says whether a soft-wrapped row ends in the blank a
wide glyph did not fit in, which a copy leaves out (a blank erased in a
colour is a cell, and stays; a double-width row ends in the last column of
its half). `Row::glyphs` gives a row's glyphs with their columns, a wide
glyph's second half and that spacer left out, and `Row::text` their text,
blanks as spaces: the texts of a line's rows, joined while
`Row::continues`, are the line's. A row that ends its line stops at its
last cell that is not a default blank, so reading it costs what it holds,
not the screen's width.

Marks (`Screen::mark`) are not consumed, so readers share them. A mark
taken after any processing differs from one taken before;
`full_refresh_since` says whether something structural happened since
(scroll, resize, reset, screen switch, history eviction), and
`Row::changed_since` whether a row is newer than the mark. A stale mark
forces a refresh; it never hides an update. `Screen::revision` counts the
bytes of output read, one held back while work is left over, and the
resizes to another size and changes to history: with no work left, the
same however the output was cut into calls, where marks move on with
each call. `Screen::history_numbers` numbers the primary screen's history
rows: each row it takes gets the next number, so a host that noted the
end finds the rows scrolled in since, and whether any went before it
read them; a resize may number history afresh. `Screen::scrolled_since`
counts the rows scrolled up off the top of the screen shown since a mark,
on either screen, with history or none: a host mirroring the screen moves
the rows it sent up by that many, then sends those whose id or version
differ.

## Bounds

What a program's bytes can make fux-vt hold or do, whatever they are and
however they are split, under what the host promises. Each line is a row
of the bounds suite (`tests/bounds.rs`), which feeds its reproducer below,
at and past the limit, at 1-byte, 16-byte and 4 KiB pieces, and checks
fux-vt's work counter and a counting allocator against it. The threat
model, every row with its limit, input and test, and the memory and work
formulas are in [`THREATS.md`](https://github.com/gold-silver-copper/fux/blob/master/fux-vt/THREATS.md).

- **Pictures** (B01) keep at most 96 MiB of pixels per terminal
  (`PICTURE_BYTES`, `Parser::set_picture_bytes`), on both screens, the
  picture being drawn and its growth included, and what newer pictures
  cover of them. Past it, pictures no longer shown go first (covered,
  scrolled away, or on the other screen), then the oldest.
- **Covering** (B46): a picture placed covers at most 4,000,000 words of
  64 pixels of the pictures under it (`COVER_WORDS`); past it, one under
  it goes whole.
- **Pictures, by count** (B02): at most 64 on both screens together
  (`MAX_PICTURES`); the 65th lets one go, those no longer shown first.
- **A host's pictures** (B06): a picture the host keeps, as its
  `Arc<Picture>` from `Screen::pictures`, keeps its memory, outside the
  budget, until the host lets it go; fux keeps a paint's worth a client.
  A parser cloned while a picture is being drawn copies it, the clone's
  own.
- **A sixel picture** (B03, B04) keeps at most 4096 by 2160 positions of
  its grid, 4096 a side, and is shown at its aspect, at most 4096 pixels a
  side; what is past that is not painted: its pixels take at most
  35,389,440 bytes, and 53,084,160 while they grow. Its room grows by
  doubling each side, so decoding paints and copies at most 4,096 pixels
  a byte.
- **A picture's declared size** (B05) costs nothing until it is drawn:
  the raster attributes' extent and the background are kept as numbers,
  and read as the picture is shown. Placing a picture fills its cells, at
  most about 2,300 cells a byte at a 10 by 20 cell and 7,600 at the
  smallest cell a host may give, 2 by 6 (`CellSize::LEAST`).
- **History** (B17) takes at most `history_bytes` of memory, its rows'
  records, blocks and maps included, and at most half as much again
  while its storage grows; a lowered budget keeps the newest rows that
  fit, in storage made for it. A map gives its room back once its
  capacity passes 4 × its length + 64.
- **Replies** (B28) are at most 16 bytes for each byte that asked, the
  longest DECRQSS's of a pen with every rendition and three direct
  colours, and XTVERSION's, 55 bytes for 4; each is made where it is
  sent, synchronously, so fux-vt queues none.
- **A resize** (B37) builds the new screens and history before it lets
  the old go, so while it runs it needs up to history's memory again and
  the new screens' (12.1 MiB more at 50 by 200 with 8 MiB of
  history), and reads, copies and clears each cell about three times; a
  refused one changes nothing.
- **`Cells`** (B39) holds at most 64 Mi cells, as a screen does:
  `Cells::new` and `Cells::reset` refuse more
  with `Error::Capacity`, and so does a lack of memory, the cells left
  as they were.
- **Listing pictures** (B09) doesn't search history: each picture's top
  row is followed into history as it scrolls there, by its number in the
  order rows came, so `Screen::pictures` reads at most the screen's rows
  a picture, and the cells only of a picture's rows changed since it was
  last found whole. Placing a picture checks the others the same way.
- **Styles** (B12): a program can't choose colours that collide in the
  style table: its hash takes both of a style's words, keyed once a
  process from std's random keys.
- **The style table** (B11): its bytes past what the screen alone may
  need (twice its cells' styles) count in `history_bytes`, so the oldest
  history rows go as it grows, and when they all have, the styles no cell
  shows go. The screen's share is 1,146,880 bytes a grid at 50 by 200.
- **The style sweep** (B13): the table is swept once it holds twice the
  styles in use at the last sweep, and at least max(4096, cells / 8); a
  sweep visits at most twice the screen's and history's cells (2,117,152
  at fux's defaults), so it costs at most 32 cell visits for each new
  style, amortized, and at most 7 a byte.
- **Hyperlinks** (B14, B16): each screen holds at most 4 MiB of URIs and
  ids (`LINK_BYTES`, real heap), a URI at most 2,083 bytes and an id 250.
  Making room for links costs at most 256 steps a byte, amortized: after
  a sweep that frees too little, the next 256 links are refused untried,
  their text printed unlinked.
- **Strings fux-vt doesn't keep** (B21): APC, PM, SOS, a DCS it doesn't
  answer and an OSC no option asks for cost nothing to hold and a step a
  byte to read; a DECRQSS request keeps its first 4 bytes, and a longer
  one is answered as not known.
- **REP** (B33, B34, B34a): copies past a screen and history's worth are
  skipped, with the same result, and so are copies that only join a full
  cluster, so REP writes at most 14,564 cells a byte for an ASCII
  character and 23,832 for another.
- **Functions that touch the whole screen** (B35, B36): ED, the
  rectangle fills, erases, copies and checksums, DECALN, 1049's clear,
  DECIC, DECDC, SL and SR touch at most N / 2 cells a byte, N the
  screen's cells (5,000 at fux's defaults), uniform fills and clears a
  row's worth; DECCRA and a scroll inside left and right margins copy at
  most 124,100 text bytes a byte at the defaults, each row they write
  compacted once (THREATS.md B35). A scroll inside a region (IL, DL, SU,
  SD, and IND and RI at its margins) turns the region's rows once
  whatever the count, which is clamped to the region's rows: it moves at
  most twice the region's rows and clears at most its cells.
- **Finding a row by its `RowId`** (B40) looks through the screen's rows,
  then history's index of spans of consecutive ids (`history/spans.rs`):
  at most the screen's rows and a binary search's, ⌊log2(history rows +
  1)⌋ + 1, a call. The index's first 256 spans are free (20 KiB a grid);
  each past them costs 80 bytes of history's budget.
- **`ByteQueue::push_with`** (B41): its closure may only add bytes; one
  that takes them away leaves a queue that still takes what is pushed
  next.
- **Small fixed state** (B42): tab stops take a byte a column, the colours
  about 1.3 KB and the kitty keyboard stacks 32 entries each.
- **Events** (B43) go to the host's `Sink` as they happen, each at most an
  OSC payload (64 KiB); fux-vt queues none.
- **Finding a frame's end** (B19): `FrameScan::end` reads each byte at
  most twice (a CSI's are searched for its final byte, then those of the
  few that may end a frame read), however the frame comes split, a
  sequence left unfinished included.
- **A cluster** (B30, B31) keeps at most 128 bytes,
  and a row's text at most 32 bytes a column and 128 (long clusters) and
  17 a column (short); joining a character reads its cluster again, at
  most 256 text bytes a byte, and a character a full row can't take
  costs no copy of its text. The longest RGI emoji sequence is 35 bytes;
  the corpus's longest cluster, 14 (a ZWJ flag, in helix-unicode).
- **A glyph** (B32) takes at most 2 columns: U+17D8, the one character
  unicode-width makes 3 wide, takes 1, as in xterm 411 and glibc
  (`char_width`; `clusters` and `str_width`, which fux's painter uses).
- **Width.** fux-vt's width rule is its own, generated with its
  segmentation tables from the Unicode Character Database 17.0.0 by
  `fux-vt/gen`, one lookup a character in the same table. A character
  takes the columns unicode-width 0.2.2 gives it (zero for its zero
  widths, 2 for East Asian Wide and Fullwidth, else 1; controls none),
  U+17D8 aside, and the 34 spacing marks (General_Category Mc) of
  Canonical_Combining_Class 0 it makes zero as Grapheme_Extend (U+09BE
  BENGALI VOWEL SIGN AA), which take 1, as wcwidth 0.9.2, ucs-detect's
  reference, measures them: a consonant and its spacing vowel sign take
  two columns. The 12 of another class stay 0: the spacing viramas (as
  wcwidth 0.9.2 has them; glibc gives them 1), and the Hangul tone marks,
  Vietnamese reading marks (East Asian Wide; glibc gives them 2) and
  musical stems and flags, which canonical ordering moves among marks. A grapheme cluster takes unicode-width 0.2.2's
  string width of that cluster alone (`src/unicode/width.rs`: emoji and
  text presentation, modifiers, ZWJ, tag and keycap sequences, flags,
  Kirat Rai's vowels), but without its rule that a Khmer conjunct takes
  one column (the coeng takes none and each letter one: two, as wcwidth
  0.9.2 and glibc's widths summed); and a cluster holding such a spacing mark takes the
  columns of its form composed (each pair of a character and such a mark
  with a canonical decomposition made one, `tables::COMPOSITIONS`, as
  NFC composes them: such a mark is of class 0, which canonical ordering
  never moves and any character between blocks), such a mark right after
  another spacing mark taking none, so canonical equivalents take the
  same columns (U+09CB and U+09C7 U+09BE). Its
  ligatures between clusters (Arabic Lam-Alef and the like) take no
  part, as a cluster is laid out alone. It is a recorded tailoring of
  UAX #11, which leaves a terminal's widths to it, chosen to match what
  programs laid out with unicode-width expect, and, for spacing marks and
  Khmer conjuncts, with wcwidth 0.9.2's (ucs-detect: 83 of 85 languages,
  where unicode-width's rule gives 77; a Tai Tham (Khün) conjunct
  and one Malayalam chillu sequence still differ). Tested against unicode-width 0.2.2 for every code point and
  some 15 M clusters, its departures aside (`unicode::tests`), and
  against NormalizationTest's canonical equivalents (`tests/ucd.rs`).
- **An OSC string** (B20) keeps at most `OSC_PAYLOAD_LIMIT` (65,536)
  bytes, read to its end however long; its buffer holds at most 65,537
  (65 with only `PromptMarks`, 1 with no feature that reads OSC), and
  81,921 while it grows.
- **The work of one call**: `Parser::process_until_stop` stops as
  `Stop::Budget` once it has spent 1,000,000 units of work since its last
  such stop, over as many calls as that took (THREATS.md 4). A unit is an
  element visited: a cell, a row, a row slot, a text byte, a pixel, a
  style or link entry; every loop the input reaches spends what it visits.
  It looks where a sequence or a string ends, after a run of text, a
  character read alone or a control, after each sixel byte, at each
  wrap, and after each glyph inserted (IRM) or printed one at a time; a
  call that ends a frame past the quota leaves its stop to the next, made
  before any byte. So between two budget stops it spends at most the
  quota and the one indivisible operation that crossed it: a screen-wide function, a sweep
  of history, a picture's growth or placement; at most 2,013,047,090 units
  at a 4096 by 4096 pane with 1 GiB of history (THREATS.md 4.2). A count
  is not a time: at the largest panes one such operation alone can take
  far longer than a frame (THREATS.md 4.3). The host gives the rest of the
  input later, and calls again while `Parser::has_work_left` (REP's
  copies left), even with no byte left. A host that holds no frames and
  spreads no work over turns (a replay, a converter, a test) calls
  `Parser::process_all`, which makes those calls itself until every byte
  is read and no work is owed, and gives how many times room was refused
  on the way (B44): the screen is good to read whatever the count.
- **Encoding a picture** (B08): `sixel::encode` reads each pixel of its
  crop twice and sets each sixel once, whatever its colours, and writes at
  most 12 bytes a pixel and 6 a colour a band of six rows.
- **Running out of memory** (B44): what grows with a program's output is
  asked for so that a refusal can be taken: history's rows, a screen's
  cells, a picture's pixels. A refusal loses only what it was for, the rest
  of the input is read, and `Parser::refused` counts it. Everything else
  fux-vt allocates is bounded by the screen's size and the limits above (a
  row's text and links, the style table, the OSC buffer, tab stops, the
  colours, DECCRA's copy of the screen); if the system can't give one of
  those, the process aborts, as Rust's allocations do.

## Verification

- **Tests** (`cargo test -p fux-vt`), named in each row above;
  `tests/properties.rs` and `tests/invariants.rs` check against small
  independent models and invariants over generated input;
  `tests/versions.rs` checks identities and versions. Expected values come
  from the references or xterm, never from fux-vt, and each test cites its
  source.
- **The `checks` feature**, off by default and for tests and fuzzing
  only: `work_spent`, the budget's own deterministic count of the work
  fux-vt does, which the bounds suite (`tests/bounds.rs`) holds each call
  to the quota and one indivisible operation, and each rate a bound
  states to its constant; and `Parser::check`, one invariant checker over the parser,
  both screens, their history, styles, links and pictures, which
  says which checks ran and what each found. The invariant tests, the
  oracle (`diff/`) and the fuzz targets call it after every operation.
  Without the feature none of it is built.
- **Fuzzing**: five targets in
  [`fuzz/`](https://github.com/gold-silver-copper/fux/blob/master/fux-vt/fuzz/README.md)
  (`terminal`, `deep`, `keys`, `graphemes` and `cells`), explored nightly
  in CI from cached corpora (`fuzz.yml`), beside the oracle's.
- **Other terminals**:
  [`compare/`](https://github.com/gold-silver-copper/fux/blob/master/fux-vt/compare/README.md)
  runs fux-vt beside Ghostty, alacritty, libvterm, avt, wezterm, vt100,
  xterm.js, tmux and xterm on random cases, recordings of real programs and
  esctest, failing where they outvote it; a family a standard or xterm
  decides is checked by the standard's test or beside xterm
  (`run.sh verdicts`). `compare.yml` runs it on request and writes a
  scoreboard of the results into its run's summary.
- **Speed**: `bench/gate.sh` counts instructions against master on a
  quiet machine (`bench/README.md`).

## Where the standards leave the choice

Here the standards are silent or leave the behaviour to the
implementation, and fux-vt does what xterm 411 does. Each item ends with a
case to replay (`fux-vt/compare/run.sh replay --engines all --size RxC
'BYTES'`).

- **Bold and faint can both be on.** ECMA-48 (8.3.117) names 1 "bold or
  increased intensity" and 2 "faint, decreased intensity or second
  colour", and leaves their combination to the implementation; 22 ends
  both (`1x3 '\e[1m\e[2mX'`).
- **REP after a grapheme cluster** repeats the character that took the
  cell. ECMA-48 (8.3.103) repeats the preceding graphic character and
  knows no cluster (`1x8 'e\u{301}\e[2b'`).
- **REP after a control function** repeats nothing until a character is
  printed; ECMA-48 leaves it undefined (`1x8 '-\e[2b\e[2b'`).

One departure from the standards remains, from mode 2027's spec
(`references/modern/mode_2027_grapheme_clusters.tex`, "Grapheme
Cluster"), whose emoji rules its join rule cannot meet:

- **A cluster takes its string width** (the width rule's) as characters join
  it, where the spec moves the cursor on a join only for VS16. Its
  "Emoji" section wants a flag (two regional indicators, each one column
  alone) and an emoji modifier sequence two columns, which no VS16 makes;
  and a conjunct, or a Prepend character with what follows, is drawn in
  its string width by a host terminal that measures so, as Ghostty draws
  conjuncts and flags (`1x6 '\u{600}1x'`: two columns, where the join
  rule would leave one).

And from UAX #29, whose clusters a row of cells cannot always hold:

- **A character of no width that joins no cluster** has no cell of its
  own: it joins the cell before the cursor, and is dropped at a row's
  start; what follows it is segmented after it, as UAX #29 segments the
  cell's text (`1x6 '\u{1f1f5}\u{11a8}\u{1f1ef}'`: the indicator after the
  jamo is a cell of its own). UAX #29 makes it a cluster of its own (GB4, GB5 and GB999), which
  no cell can hold at no width. These are the zero-width Prepend
  characters (U+0605 and 18 more), which so stay apart from what GB9b
  joins them to (`1x6 'x\u{605}A'`: `x\u{605}`, `A`; xterm, through
  glibc, gives them a column of their own); the format controls of
  Grapheme_Cluster_Break Control (SHY, ZWSP, the bidi marks, WJ to
  U+2064, U+FEFF), as alacritty, libvterm, vt100 and tmux keep
  `a\u{200B}b` (xterm and Ghostty drop ZWSP); a lone vowel or trailing jamo after anything but a leading one;
  and U+3164 and U+FFA0, the Hangul fillers.
- **A mark after a glyph that was dropped** (wider than the screen, or
  past the margin with autowrap off) joins the cell before the cursor, as
  in xterm, Ghostty and tmux (`1x4 '\e[?7labc\u{4e00}\u{301}'`: `c\u{301}`),
  where GB9 makes it part of the dropped glyph's cluster.
- **A control does not end the cluster** but BS, HT, LF, VT, FF and CR,
  which move the cursor: a mark after BEL or NUL joins the cell before it,
  as in xterm, Ghostty, alacritty, libvterm, vt100 and tmux
  (`1x4 'e\x07\u{301}'`: `e\u{301}`), where GB4 and GB5 break around a
  Control. The control is carried out, not stored as text.
- **A cluster's width is the width rule's, at most 2**: one wider (four
  leading jamo, `ᄀᄀᄀᄀ`, GB6; U+0600, a Prepend character of one
  column, before a wide glyph)
  takes one cell of two columns. A cell once wide stays wide: a text
  presentation selector (U+FE0E) after an emoji shown wide leaves it wide.
  A lone regional indicator takes one column and a lone emoji modifier
  two, as unicode-width has them. xterm keeps a cell per character
  of its own width (`1x6 '\u{600}\u{4e00}x'`: three cells there, one
  wide cell here).

And one choice where fux-vt departs from xterm, as no standard decides it
under UTF-8:

- **A non-ASCII character inside an escape sequence or a CSI** is
  ignored, the sequence going on: `ESC é x` is `ESC x`, and `CSI 1 é C`
  is CUF 1, as in Ghostty, alacritty, wezterm, vt100 and tmux. DEC STD
  070 (3.5.4.5) reads a GR byte in a sequence as its GL equivalent, a
  rule for 8-bit codes with no meaning once UTF-8 is designated (ECMA-35,
  15.4); xterm 411 applies it to the characters U+00A0 to U+00FF it
  decodes (`CSI 1 é` is `CSI 1 i`, MC, and the `C` printed) and drops the
  sequence at any character past U+00FF, which would make a sequence's
  meaning hang on how a character was encoded (`1x8 'a\eéxb'`: `ab`
  here and in five engines, `axb` in xterm, avt and xterm.js).

Other readings to keep, each one a later change could take for a bug.
Each item names the test that holds it:

- **SUB shows its error character.** DEC STD 070 contradicts itself:
  SUBSTITUTE's own page (p. 5-132, note 2) and the C0 table (5.9.1) show
  the reversed question mark, but chapter 3's parsing algorithm cancels
  the sequence and shows nothing. fux-vt follows the function's own page,
  as xterm 411 does. The other engines drop SUB as they drop CAN, and
  outvote fux-vt (`1x6 'a\x1ab'`;
  `conformance::sub_is_displayed_as_the_error_character`).
- **CAN inside sixel data ends the picture unplaced.** CAN ends a control
  string "without execution" (DEC STD 070, 3.5.1.2.1). The sixel
  chapter's "exit Sixel mode" (9.1) says nothing about placing what was
  drawn. Only ST places it, so a program that cancels a picture shows
  none (`extended::sub_inside_sixel_is_a_blank_sixel_and_can_ends_it`).
- **Sixel data is read a byte at a time.** Each byte of a UTF-8
  character is a GR code, read as its GL code (DEC STD 070-9, 9.4), so
  `é` (C3 A9) is the sixel `C` and then `)`. xterm 411 reads it the same
  way: it stops decoding UTF-8 inside control strings (`charproc.c`,
  `utf8_inparse`). Decoding UTF-8 there would be a departure from xterm,
  not a fix (`sixel::tests::gr_codes_are_read_as_gl_and_c1_codes_ignored`).
- **A picture whose top scrolls off a region's top margin is cropped
  there.** The rest stays shown (DEC STD 070-9, 11.2.4), where xterm 411
  moves every graphic up whatever the region (`scroll_displayed_graphics`).
  The crop is recorded when the scroll happens, so margins set or reset
  later change nothing
  (`extended::a_picture_whose_top_scrolled_off_a_region_shows_what_is_left`,
  `extended::a_pictures_crop_is_the_scrolls_not_the_margins_set_after`).
- **A row scrolled down to the screen's last row keeps its soft wrap**,
  as xterm 411, alacritty and WezTerm keep it. xterm's printout ends its
  last row with no line feed, so a comparison that reads the printout sees
  that row as unwrapped, and it is not (`5x1 --history 0 'll' '\e[4T'
  '\e[2S'`: row 2 stays soft-wrapped;
  `semantics::a_row_scrolled_down_to_the_last_keeps_its_soft_wrap`).
