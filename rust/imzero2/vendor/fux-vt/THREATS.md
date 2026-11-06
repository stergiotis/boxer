# fux-vt: threats and bounds

What a program in a pane can make fux-vt do, and the numbers that hold
it back: the threat model (1), every bound with its test (2), memory
(3), work and the quota a call spends (4), and the
checker's invariants (6). The code and tests cite it by row (B01 to
B46 but B07, B10 and B29, which are gone, and B34a) and by section number.

"The defaults" are fux's: a 50×200 screen (N = 10,000 cells, R = 50
rows, C = 200 columns) and H = 8 MiB of history.

## 1 The threat model

### 1.1 What a program in a pane controls

- Every byte it writes, in any order, amount and chunking. fux feeds
  what it read whole (16 KiB a read; a held frame, or a program's last
  output as it exits or is respawned over, more); a library host may
  pass a whole file at once.
- Through those bytes, all the state they reach: modes, margins, the
  pending wrap, wide glyphs, protection, both screens, styles,
  hyperlinks, prompt marks, OSC payloads, DECRQSS requests, sixel data,
  REP counts up to 65,535, and how long a sequence stays unfinished.
- Not the timing: `process_until_stop` has no clock, and the key decoder's
  deadlines come from the host's calls.
- Not host calls, hash keys or addresses. Replies never carry memory.

The key decoder reads the client's own terminal, not a pane, but a
paste's content is whoever filled the clipboard's, so it is hostile too.

### 1.2 What the host controls and promises

fux-vt's own limits: rows and columns are `u16`, and rows × columns ≤
`MAX_CELLS` = 67,108,864, else `Err(Capacity)` and nothing changes
(B38). History holds at most `MAX_ROWS` = 1,048,576 rows and `MAX_CELLS`
cells, whatever its byte budget.

fux's promise, within which every bound and the quota hold:
a pane is at most 4,096 rows × 4,096 columns (`view::MOST`: a client's
screen less its bar, or a tab given its size by `size` or `resize`; the
probes run at 4,095 × 4,096);
history is at most 1 GiB (8 MiB by default); fux gives a pane's parser
at most 16 KiB at a time (`fux::pane::SLICE`): one read, or a slice of
what waits (`Pane::unread`), a held frame, shown once past 2 MiB
(`FRAME_LIMIT`), or a program's last 1 MiB as it exits or is respawned
over, the rest left for the pane's later turns as at a budget stop; the
fixed `Feature`s are `fux::pane::OPTIONS`', with reflow and pictures selected by each pane's checked creation policy.
`ExtendedReplies` and `RectangleChecksums` stay off.

The host also sets the picture budget (`Parser::set_picture_bytes`,
96 MiB by default, B01; set lower, large pictures clip), its cell size
(`set_host_graphics`; `CellSize` refuses one under 2×6 pixels, so that
a picture's placement covers at most 683 rows, B05), the paste limit
(`with_paste_limit`; `usize::MAX` removes it) and the `Feature`s, which
set the OSC limit (B20).

Each host call has its own bound per call: `resize` (B37), `row_index`
(B40), `pictures()` (B09), `sixel::encode` (B08), `Cells::new` and
`Cells::reset` (B39), `FrameScan::end` (B19),
`ByteQueue::push_with` (B41). The host promises its own call rate, and
that `push_with`'s closure only adds (B41 makes a broken promise
harmless).

Objects the host retains are its memory, not the parser's budget:
- an `Arc<Picture>` outlives its eviction (B06); fux keeps each client's
  last paint, at most one budget's worth more per pane per client;
- a cloned parser deep-copies grids, history, text, links and buffers,
  and shares pictures and the style table until either side adds a
  style; a clone taken mid-picture copies the picture being drawn
  (≤ 33.75 MiB);
- events and replies go to the `Sink` synchronously; fux-vt queues none;
- the key decoder's `out` grows at most one `Input` a byte (B26).

Shell marks (OSC 133). Every mark in the output counts, whichever
program sent it: any program can end the command running, with a status
of its choice, or start one. A mark costs a fixed amount of work and no
memory.

### 1.3 What happens when an allocation fails

The program-sized allocations are fallible: each degrades where it
happens, counts one in `Parser::refused()`, and the call reads every
byte it was given. It returns `Err(Error::Capacity)` only to say that a
refusal happened in it.

| Site | Degrade |
|---|---|
| A row scrolled into history | The row goes as with no history; the screen still scrolls |
| The alternate screen | The primary stays shown, and the mode reads reset |
| RIS or a clear, with no room for new grids | History goes, then the grids are blanked in place |
| The sixel decoder's growth | The picture keeps what it has, as at the budget (B03) |
| The ring or a map shrinking | It keeps its room until the next trim (B17) |
| A prompt mark's or a picture's line feed | The parser's state is set first, so it is never stuck in the string |

Everything else allocates infallibly, and a failure there aborts the
process (no unwinding). Those allocations are bounded by the screen's
size and the crate's constants, not by what a program sends (a row's
text, its link runs, the style table's screen share, DECCRA's copy, the
OSC buffer, tab stops, the key decoder's buffers): a program can fill
them, not grow them. The bounds keep both the abort and the OOM killer
away; infallible allocation is no safety proof.

### 1.4 What happens at a limit

| Kind | Meaning | Rows |
|---|---|---|
| Clip | What fits is kept | B03, B04, B20 |
| Evict | The oldest, or the hidden first, goes | B01, B02, B11, B15, B17, B27, B46 |
| Drop to its end | A string past its limit is read to its end and dropped, alike however split | B20's rest, B21, B23, B25 |
| Refuse | `Err` or `None`, and nothing changes | B37, B38, B39 |
| Skip work that changes nothing | Copies that can't change the result are skipped | B33, B34, B34a |

## 2 The bounds

Conventions for every row:
- Chunkings: each probe feeds its input at 1 B, 16 B and 4 KiB a call,
  and whole (`WHOLE`: all of it, given again from each stop), as
  `tests/bounds/probe.rs` `CHUNKS` lists. Rows with megabytes of setup
  feed at no less than 16 B or 4 KiB, as each probe says.
- Sizes: 1× is the smallest input that reaches the limit; 10× and 100×
  repeat it. Each row is one test named for its property, which runs 1×
  and 10× in CI; with `FUX_VT_BOUND_STRESS=1` it runs 100× too, at
  release under a 2 GiB memory cap.
- Each probe asserts, after every call: the heap's peak against the
  row's bound (from section 3); the work between two budget stops, frame
  boundaries between, within the quota and one indivisible operation, a
  budget stop no sooner than the quota, and some work in a call that
  takes no byte but the stop a frame's boundary owed (4.2); and, for a row that
  states a rate in units, the work so far against it. Most also assert
  a lower bound on the work they know was done (4.2). At the end, the
  invariant checker is clean (section 6).
- Features: every `Feature` on, unless the row says otherwise. Screen: the defaults, unless the row says.
- Seeds: every row's reproducer is a frozen seed in `tests/seeds.rs`
  (written to `fuzz/seeds/`), replayed by
  `every_seed_reaches_its_limit` to check it reaches its limit.

In the test column, `bNN` is the probe of that name in
`tests/bounds/rows.rs`, run by the test in `tests/bounds.rs` named for
the row's property. Work is in 4.1's units. B06, B08, B09,
B26 and B41 bound host-side objects or host calls.

| ID | What is bounded | The limit (counter) | At the limit | Input that reaches it | Test |
|---|---|---|---|---|---|
| B01 | Pictures kept, in bytes | Kept pictures' pixels on both screens, with what newer ones cover of them (B46), + the picture being drawn + its growth transient ≤ P = `PICTURE_BYTES` = 96 MiB a parser (holds one 4096×2160 picture with its growth) | Pictures no longer shown go first, then the oldest; a decode alone past P clips (B03) | `ESC P q "1;1;4096;2160 ESC \` × 64 | `pictures_hold_no_more_than_their_budget`, `pictures_at_the_budget_are_kept_and_a_byte_past_lets_one_go`, `pictures_no_longer_shown_go_first` (`screen/tests.rs`); `b01` holds the heap with extents declared and nothing drawn, so it does not reach the budget |
| B02 | Placements, by count | ≤ 64 on both screens together (`MAX_PICTURES`) | The 65th lets one go, as B01 picks | 65 × `ESC P q @ ESC \` | `b02`; `a_screen_keeps_64_pictures_and_the_65th_lets_the_oldest_go` |
| B03 | The sixel decoder's buffer and growth | Buffer ≤ `MAX_PIXELS` × 4 = 35,389,440 B; each side doubles, clamped to `MAX_SIDE` = 4096 and `MAX_PIXELS` = 4096 × 2160; growth copies or zeroes ≤ 2 × the final buffer (≤ 4,096 units a byte + 2 × `MAX_PIXELS` a picture), doubling even beside pictures that fill P, which it lets go for the room, while P holds twice the picture (always at the default P; a host's lower P may make it grow just to what it needs). Raster attributes grow nothing | Sixels past the limits or P aren't painted; the picture keeps what fits | A picture growing a column at a time over 340 bands; width and raster ladders `"1;1;w;2160` | `b03`; `a_kept_picture_holds_just_its_pixels` |
| B04 | Sixel decode work | ≤ 4,096 units per input byte; pixels are kept at grid resolution, the aspect applied when shown; a repeat past 4,096 counts 4,096 | — | `ESC P q "360;1` + `!4096~$` × 600 | `b04` |
| B05 | A declared extent, and placement | An extent with nothing drawn allocates and paints nothing; covering the last cell row of the extent before, as each does, is B46's (70,656 B a picture at a 10×20 cell). Placement is one indivisible operation (4.2): its rows filled, ⌈height ÷ the cell's⌉, at most 683, each ≤ 3C + 6 (a unit, its fill, its line feed into history), ≤ 9,492 to cover the one before, and between margins one scroll of them; a host cell under 2×6 refused (`CellSize::new`) | — | 215 × `ESC P q "1;1;4096;2160 ESC \` in 4 KiB, P2 = 0, 1, 2; and portrait | `b05`; `a_declared_extent_keeps_no_pixels`, `a_picture_needs_a_host_cell_of_a_sixel_band_or_more` |
| B06 | Pictures the host retains (host side) | An `Arc<Picture>` outlives eviction, outside the budget; a clone mid-picture ≤ steady + 2 × 53,084,160 B | — | A clone mid-picture; a lowered budget while the host holds one | `b06` |
| B08 | `sixel::encode` (host call) | Output ≤ 12 B a crop pixel + 6 B a colour a band + colour definitions; work ≤ 3 a crop pixel. fux caches encodings per client, ≤ 16 MiB (`SIXEL_BYTES`) | — | A 2000×1000 256-colour checkerboard | `b08` |
| B09 | `Screen::pictures()` (host call) | Work ≤ 128 × (⌊log2(history rows + 1)⌋ + 1 + R) + 64 + the rows its placements cover, a call | — | 8 MiB of empty history, 64 pictures; 64 pictures 4,096 pixels tall | `b09` |
| B11 | The style table, in bytes | Its room past the screen's share (S(2N): 1,146,880 B a grid at 50×200) counts in history's bytes; a successor grid or a cloned parser's copy is charged at once | History's oldest rows go | 150,000 distinct direct backgrounds, 1 MiB of history | `b11`; `a_style_table_past_the_screens_share_takes_history_room` |
| B12 | Style lookups | Expected O(1) probes: std's keyed hash of both words; ≤ 64 styles share a place (`SPREAD`, checked) | — | A family of 8 Ki to 32 Ki styles that hashed alike unkeyed | `b12` |
| B13 | The style sweep | ≤ 7 per input byte, amortized: a sweep after keeping L styles comes after max(floor − L, L) new ones, floor = max(4,096, N ÷ 8) | — | 20,000 new backgrounds on one cell over 700 full rows | `b13`; `deccara_maps_each_of_three_styles` |
| B14 | Hyperlinks held | ≤ `LINK_BYTES` = 4 MiB a grid of real heap (strings, `Arc` headers, the id map's buckets, both tables while it grows); URI ≤ 2,083 B, id ≤ 250 B | A link that doesn't fit after a sweep prints unlinked; after a failed sweep the next 256 are refused untried (`STALL`) | 2,000 links of 2,083-byte URIs; 60,000 1-byte links with distinct ids | `b14` |
| B15 | Link runs | A screen row ≤ 256 runs × 32 B at 200 columns; a history row's runs shrink as it enters and count in history's bytes | History's oldest rows go | Two links alternating cell by cell, 1 MiB of history | `b15`; `a_row_with_one_link_holds_no_memory_for_it` |
| B16 | The link sweep | ≤ 256 per input byte, amortized; its transient is the links kept, 16 B a run, nothing per history row | — | 4 MiB held, then refused opens; the same over 400,000 empty rows | `b16` |
| B17 | History, in bytes | `History::bytes()` counts capacities (ring, blocks, text, maps, side stores, link runs, styles), trimmed to ≤ the budget; a map gives back room past 4 × its length + 64; heap ≤ 1.1 × H + 64 KiB | The oldest rows go | Rows of 2,049 cells; 349,525 empty rows, then full ones | `b17`; `guards.rs` `history_*` |
| B18 | Line feeds into history | ≤ C + 5 per line feed, amortized | — | 100,000 `\n` after full rows | `b18` |
| B19 | The frame scan | Work ≤ 2 × the bytes held + 16 a call | — (fux cuts a frame at 2 MiB or 1 s) | BSU, then `ESC [` + 64 Ki digits in pieces; a long DECRST | `b19`; the fuzz targets' frame-end agreement (`common.rs`) |
| B20 | The OSC buffer | Room ≤ the OSC limit + 1: 65,537 B (`OSC_PAYLOAD_LIMIT` + 1), 65 B with only `PromptMarks`, else 1 | A longer string is read to its end, its event `cut` (`Kept`), nothing past the room kept | OSC 52 of 64 KiB + 1 | `b20`; `the_osc_buffer_takes_the_room_a_string_needs_up_to_16_kib`, `the_osc_buffer_has_room_for_its_limit_alone` |
| B21 | DECRQSS, APC, PM, SOS, ignored DCS | DECRQSS keeps ≤ 4 B (`REQUEST_LIMIT`); the rest keep 0 B | A longer request answers "not known" | 64 KiB of APC; sixel with `Pictures` off | `b21` |
| B22 | Parameters | 32 `u16` per sequence, no heap | A 33rd isn't kept | `CSI` + `1;` × 1,000 + `m` | `b22` |
| B23 | The paste decoder | A paste's room ≤ limit + 5 = 65,541 B; the delivered `String` ≤ 3 × limit; work ≤ 2 a byte + 258 a read | `PasteTooLong`; the rest dropped to the end marker | `ESC[200~ESC[201~` × 5,461 in one 64 KiB read | `b23`; `a_paste_of_64_kib_is_kept_and_one_byte_more_is_not` |
| B24 | Bytes the key decoder holds | ≤ `HELD` = 258 B (+ ≤ 2 × 258 B while a read is cut); work ≤ 2 a byte + 516 a read while an answer is held | — | `ESC`, then 1 MiB of `a`; a 258-byte DCS answer 1 byte at a time | `b24`; `key_work.rs` |
| B25 | Answer windows | OSC ≤ 128 B (`OSC_LIMIT`), DCS ≤ 256 (`DCS_LIMIT`), key CSI ≤ 32, `CSI ?` ≤ 64; past a window held to 256 B, then dropped | Dropped to its end, nothing typed, however split | `ESC [ ?` + 300 × `1` + `c`, whole and in pieces | `b25` |
| B26 | The key decoder's output (host side) | ≤ one `Input` per input byte | — | 64 KiB of single keys | `b26` |
| B27 | The kitty keyboard stack | ≤ 32 entries a screen | The oldest goes | 33 × `CSI > 1 u` | `b27` |
| B28 | Replies | ≤ 16 reply bytes per input byte; each `Reply` ≤ 64 B | — | 585 × DECRQSS `m` with every rendition; each request's longest answer | `b28` |
| B30 | Cluster text | A cluster ≤ 128 B (`CLUSTER_CAPACITY`); a row's long text ≤ 32C + 128, short ≤ 17C; work ≤ 128 a byte (a mark of two reads and writes its cluster) | Cut at 128 B; a row with no room drops what would join | `e` + 64 × U+0301 | `b30`; `a_long_cluster_that_ends_at_the_limit_does_not_compact` |
| B31 | A zero-width character after a full row | A dropped character reads the cell printed last (≤ 128 B) a few times over, never the row's text: ≤ 257 a byte | Dropped | 1,000 × U+200B after a full 1×200 row and a BS | `b31` |
| B32 | Glyph width | Every glyph ≤ 2 columns; U+17D8 is 1 | — | `XYZW\r` U+17D8 on 1×10 | `b32`; `every_character_is_as_wide_as_the_width_rule_says` |
| B33 | ASCII REP | Copies past what the screen and history can show are skipped: a REP ≤ 132,775 at the defaults (65,535 copies, 328 rows into history at B18's C + 5), 30,702 between 2-column margins (102 copies, 51 scrolls of the region at 12R), 875 on 2×10 with 1 KiB of history (350 copies, 35 rows at 15); under IRM each copy moves its row; the copies made stop at the quota between glyphs | Skipped, same result | 455 × `X ESC[65535b` in 4 KiB | `b33`; `repeat_makes_its_ascii_copies_and_returns` |
| B34 | Non-ASCII REP | As B33, a copy two cells: ≤ 265,550 a REP at the defaults, 30,702 between 2-column margins, 875 on 2×10 with 1 KiB of history, 201,383,938 between 2-column margins on 4,096 rows of 3 (4,097 copies, each a scroll of the region at 12R), made over many calls | As B33 | 372 × `一 ESC[65535b` | `b34`; `repetition_stops_at_the_quota_and_goes_on` |
| B34a | REP of a glyph that joins itself | ≤ 24 a byte + 2,752 to fill the cluster once | Copies joining a full cluster are skipped | 372 × U+1100 `ESC[65535b` | `b34a` |
| B35 | Screen-wide functions | Each one indivisible operation of ≤ 12N + 2R × (49C + 128) (4.2); every call within the quota and one of them, which stops it and goes on in the next; CUB under 1045 between 2-column margins ≤ 65,535 a CUB, a unit a row it steps | — | 4 KiB each of DECRQCRA, DECCRA, DECFRA, DECERA, DECSERA, DECCARA, DECRARA, DECALN, ED 2, 1049, DECIC, DECDC, SL, SR; margin LF, SU and wrapping text | `b35` at 50×200, 500×500, 4095×4096; `bounds.rs` `quota` tests |
| B36 | Scrolls inside a region | The region's slots turned once a sequence, the count clamped to the region; across the ring's end ≤ 4 an IL of two rows | — | `ESC[4095L` at 4095×1; IL across the ring's end | `b36` |
| B37 | `resize` and reflow (host call) | Heap ≤ steady + another history term; work ≤ 6 × (history cells + screen cells + history rows) a call | A rejected resize changes nothing, synchronized output and the cluster being printed included | Full history, 200 → 199 → 200 columns; 65535×65535 | `b37`; `a_rejected_resize_leaves_synchronized_output_on`, `a_rejected_resize_leaves_a_grapheme_open` |
| B38 | The screen's size | Rows × columns ≤ `MAX_CELLS` = 67,108,864; history ≤ `MAX_ROWS` rows | `Err(Capacity)`, nothing changed | 65535×65535 | `b38` |
| B39 | `Cells::new` and `Cells::reset` | Length ≤ `MAX_CELLS` | `Err(Capacity)`, the cells unchanged | `Cells::new(usize::MAX)` | `b39`; `cells_at_the_most_are_made` |
| B40 | Row lookups by id (host call) | Work ≤ R + ⌊log2(history rows + 1)⌋ + 1 a call; the index's first 256 spans are free (20 KiB a grid), each past costs 80 B of history's budget | `None` for a row that is gone | 8 MiB of empty history; `x ESC[L CR LF` repeated | `b40`; `row_lookup.rs` |
| B41 | `ByteQueue::push_with` (host call) | After the closure, what was taken ≤ the buffer's length | — | A closure that clears the vector, then a push | `b41` |
| B42 | Small fixed state | Tab stops a byte a column; the palette about 1.3 KB; kitty stacks 32 entries | — | HTS at every column | `b42` |
| B43 | Events | Delivered synchronously; fux-vt keeps none but the last window title (`Screen::title`, ≤ `OSC_PAYLOAD_LIMIT`) | — | 400 × OSC 9 | `b43` |
| B44 | Refused allocations | Each refusal counts in `Parser::refused()` and degrades as 1.3 says; every later byte is read | 1.3's table | `ESC]133;A BEL hello` with a refusal forced (`refuse_next`) | `b44` |
| B45 | Key and mouse encoders (host calls) | ≤ 64 B per key, mouse report or `Screen::encode_mouse` chunk; alternate scroll ≤ 32,767 arrows | Default/UTF-8 positions past their limits emit a 0 byte; hosts admit or refuse each whole scroll expansion | Every key, modifier and mode; mouse at each encoding's edges; negotiated scroll expansion at maximum height | `b45`; `alternate_scroll_keeps_negotiated_arrows_in_bounded_chunks` |
| B46 | Covering | A placement covers ≤ `COVER_WORDS` = 4,000,000 words of 64 pixels of the pictures under it, the room to note them included (16 B a row shown of each picture covered, and the covered rows' words) and a copy of what was noted of one the host holds; the room, the words promised included, is inside P (B01). It reads the newer picture's drawn grid positions on the rows of those under it, once a grid row: ≤ the positions it drew (B04) | One under it past the words, or past P, goes whole, the newest covered first | 64 pictures, each a column right of the last, opaque (P2 0) and drawn (P2 1) | `b46`; `a_picture_covers_only_the_pixels_it_lies_on` |

## 3 Memory

### 3.1 The steady state at the defaults

One parser, every term a capacity:

| Term | Formula | Bytes |
|---|---|---|
| Screen cells, both grids | 2 × N × 8 | 160,000 |
| Row records, both grids | 2 × R × 104 | 10,400 |
| Row text, both grids | 2 × R × (2 × 6,528 + 2 × 3,400 + 128) | 1,998,400 |
| Screen rows' link runs | 2 × R × 256 × 32 | 819,200 |
| Links held, both grids | 2 × `LINK_BYTES` (B14) | 8,388,608 |
| Style tables, the screens' share | 2 × S(2N) (B11) | 2,293,760 |
| History, with its text, links, maps and styles | 1.1 × H + 64 KiB (B17) | 9,293,004 |
| History's free row-id spans, both grids | 2 × 256 × 80 (B40) | 40,960 |
| The parser's buffers | OSC 65,537 + the decoder + DECRQSS | ≈ 67,000 |
| The window title (B43) | `OSC_PAYLOAD_LIMIT` | 65,536 |
| Pictures, the decoder and its growth inside | P (B01) | 100,663,296 |
| Small fixed state | tab stops, palette, kitty stacks | ≈ 6,000 |

- Steady: 123,806,164 B = 118.07 MiB (22.07 MiB without pictures).
- While processing, with the largest transient (3.2): 128,419,898 B =
  122.47 MiB.
- With a resize: 136,496,768 B = 130.17 MiB, the peak engine heap per
  parser at fux's defaults.

### 3.2 Transient peaks

No two happen at once. While processing: the style sweep or a ring or
map shrinking, ≤ 0.5 × 1.1 × H (4.40 MiB); the link sweep ≤ 3.44 MiB;
a map's growth ≤ 2.64 MiB; DECCRA's copy ≈ 1.02 MiB; sixel growth is
inside P. A resize or RIS (host calls) builds a new history and screens
beside the old: + 12.10 MiB.

### 3.3 Outside the parser

- fux, per pane and client: ≤ P of evicted pictures in its last paint.
- fux, per pane: its frame buffer (≤ 2 MiB + 16 KiB, or 2 MiB + 1 MiB
  as its program's last output is read, at its exit or a respawn over
  it), and output read but not parsed after a budget stop
  (`Pane::unread`: a held frame, one 16 KiB read and a program's last
  1 MiB as it exits or is respawned over, twice while joined).
- fux, per client: encoded sixel data, ≤ `SIXEL_BYTES` = 16 MiB.
- A parser cloned mid-picture: + ≤ 33.75 MiB.
- The key decoder: 258 B held (+ 2 × 258 while a read is cut), a paste's
  65,541 B and its `String` (≤ 196,608 B), and `out` until drained.

### 3.4 How the bounds tests measure it

- `tests/bounds.rs` (`required-features = ["checks"]`) makes
  `fux-vt-peak`'s `ThreadPeak` its `#[global_allocator]`. It counts the
  live bytes and their peak for each thread apart, so the test harness's
  other threads never count, and a `realloc` counts the new block beside
  the old until the old is freed, so a growth's transient shows. It is a
  crate of its own because fux-vt forbids `unsafe`.
- One probe at a time: each holds a process-wide mutex
  (`one_at_a_time`), so the binary holds one probe's memory.
- A probe makes its parser, resets the peak (`Heap::begin`), feeds its
  input at its chunking, and after every call checks the peak above the
  base against the row's bound, built from 3.1 and 3.2 (`steady()` in
  `probe.rs` sums those terms, a little looser: 48 B a link run and 43 B
  a style entry).
- What it can't see: the allocator's own size classes and overhead (it
  counts requested sizes), and a block freed on another thread, which
  counts as freed there. fux-vt makes no threads.

## 4 Work

### 4.1 The quota

fux-vt counts its work in one per-thread count (`budget.rs`), a unit an
element visited: a grid cell written, cleared, moved or read; a row
pushed, popped, read, searched or walked; a row slot moved; a row text
byte copied or rescanned; a sixel pixel painted, filled, copied or zeroed
(a unit for four bytes of growth), a word of 64 pixels covered; a style
looked up, put in a table or swept; a link run, row, id or sort step; a
byte the key decoder or the frame scan reads. Every loop the input can
reach spends what it visits as it goes; the input bytes themselves are
not counted, as the call's length bounds what each costs. With the
`checks` feature, `fux_vt::work_spent()` reads the count.

`Parser::process_until_stop` stops as `Stop::Budget` once `LIMIT` =
1,000,000 units are spent since its last budget stop, over as many calls
as that took, so a host that cuts its reads small is stopped as well. It
looks at the count after each sequence or string, each run of text, each
character read alone, each control that spends (a line feed, a tab,
SUB, a BS that wraps back), each sixel byte, each wrap, and each glyph
printed one at a time (inserted under IRM, a mark, one that does not
fit a run): never inside an indivisible operation. A call that
ends a frame (`Begun`, `Ended`) past the quota still says so, and the
next call makes the stop it owes before any byte, so a frame's boundary
is no way past the quota. Nothing is lost: it stopped between two bytes,
and the host gives it the rest later. The one operation that goes on past a
stop is REP, whose copies stop between glyphs: the next call, made while
`has_work_left` even with no byte left, makes them before any later
byte, so replies and the screen are as one call would leave them.

### 4.2 Why that bounds a call

Between two places it looks, the parser does at most one indivisible
operation, so between two budget stops it spends at most `LIMIT` and that
operation. At a
screen of R rows, C columns and N cells, with H bytes of history, the
largest is at most M, its terms summed (`budget.rs` `call_most`, which
is `LIMIT` + M):

| Term | Units | What it bounds |
|---|---|---|
| A picture | 2 × `MAX_PIXELS` + `COVER_WORDS` = 21,694,720 | a sixel byte's growth, at most twice the final buffer (B03); a placement's covering (B46) |
| A placement's rows | 691 × (3C + 6) | at most 683 rows and 8 graphics new lines, each its fill and its line feed into history (B05) |
| The screen | 12N + 2R × (49C + 128) | a screen-wide function: 12 units a cell, and each row's text, at most 49C + 128 bytes (B30), twice (below) |
| History | 2 × min(H ÷ 8, `MAX_CELLS`) + min(H ÷ 24, `MAX_ROWS`) | the style sweep, each kept cell (8 B at least) read twice; the link sweep, each row (24 B at least) |
| Links | 2^20 | the link sweep's runs, ids and sorts (B16) |

An operation may also sweep the style table as it adds styles (DECCARA,
DECRARA): a sweep needs N ÷ 16 new styles since the last (`style.rs`), so
the sweeps inside one operation cost at most 32 units for each cell it
restyles. A rectangle restyle copies no text, so they fit the screen's
text term, which it leaves unused.

The costliest row is one a scroll inside margins or DECCRA copies, by
the charges as they are: its cells, 4C (`copy_span` reads, compares and
writes them, `copy_cells` copies the source's); the compaction that makes
room, 2C (`clear_for` walks the row for long text and for short, once:
a store that fails doesn't compact again); its links, at most 5C (the
source's runs scanned, the span's compared, then one splice, its pieces and
the runs moved past it); its edges, 2; and its text twice: the source's
copied up front, and the stores with the compaction of what lies outside,
which the row's text limit holds together, 2 × (49C + 128). That is
11C + 2 + 2 × (49C + 128), inside its share of the screen term,
12C + 2 × (49C + 128); DECCRA's links are one span set to none, at most
2C, and its row edits 3C. DECIC, DECDC, SL and SR, and DECBI and DECFI
at a margin, shift each row's links in one remake and one sort,
(⌊log2(C + 2)⌋ + 2) × (C + 2) + C units, about 15C, with the row's edit
and repair 2C; they copy no text, so like the restyles they fit the text
term they leave unused.

At fux's defaults M is about 27 M; at its largest pane, 4,096 × 4,096
with 1 GiB of history, M = 2,013,047,090, the screen's text most of it.
The bounds suite asserts for every probe that it spends at most
`LIMIT` + M at its setup between two budget stops, that a budget stop comes only once `LIMIT`
is spent, and that a call taking no byte spends some; most also assert
that the work counted is at least what they know was done, so work that
goes unspent shows. `bounds.rs`
`quota` feeds each kind of screen-wide function and REP at 4,095 × 4,096,
split and whole, to the same screen and replies. The `deep` fuzz target
and the oracle's ceiling part hold every call to `LIMIT` + M at its size
and history (`fux_vt::call_most`), which is at most 1,000,000 +
2,013,047,090.

fux reads no more of a pane after a budget stop or at the end of a
16 KiB slice (1.2): the rest waits for the pane's next turn, after the
other panes'. So one turn does at most `LIMIT` + M, whatever a pane's
program writes. Host calls
(resize, lookups, `pictures()`, `encode`) are not metered: each has its
own bound per call (B37, B40, B09, B08).

### 4.3 A count is not a time

The quota bounds a call in units, not milliseconds, and an indivisible
operation grows with the pane. Measured once, in a release build on a
Ryzen AI MAX+ 395 at 4,096 × 4,096 with 1 GiB of history: one line feed
at the bottom of left and right margins over DECALN's page took 44.6 ms,
a DECCRA of the whole screen 44.3 ms, and the 19-byte declared sixel of
B05 placed between margins at a 2×6 cell 39.5 ms, each in one call; REP's
65,535 copies under IRM took 135 calls of at most 2.1 ms (one call of
167 ms when only the copies' cells were spent), and 2 MiB of `!2000~$`
sixel fed in 16 KiB calls at most 0.11 ms a call. A page of long
clusters costs more than DECALN's. No millisecond bound is claimed at
the largest panes: one would need each screen-wide function and sweep
to stop and go on by rows. `fux-vt/compare/worst-feed.sh` measures the
costliest and the longest call over every bounds reproducer and the
corpus.

## 6 The invariants the checker asserts

`Parser::check` and `Screen::check` (under `checks`) check both screens,
and report which checks ran and each fault by its check's name. The fuzz
targets call them after every operation, the bounds probes at the end of
every row. "(Tests)" marks an invariant that compares two states, which
the tests check instead.

- **1** The OSC payload ≤ its limit (+ 1 inside an OSC); room ≤ 65,537 B.
- **2** 1 to 32 parameters.
- **3** In sixel exactly while a decoder exists, its picture in limits.
- **3a** An XTGETTCAP is answered only while its string is read.
- **4** A UTF-8 partial holds fewer bytes than its lead byte announces.
- **5** `FrameScan` resumes inside the bytes held, in a reachable state.
- **6** `refused()` never decreases. (Not checked yet.)
- **7** A grid's `order` is a permutation of its slots.
- **8** One record, text and link list per slot.
- **9** Every cell past a row's `used` is blank.
- **9a** Past a row's end, on a row not single width, every cell is empty.
- **10** Screen rows are `cols` wide; history rows fit their block.
- **11** Wide pairs are adjacent, on the screen and in history.
- **12** No glyph is wider than 2; a print advances by what it wrote.
  (Tests: B32.)
- **13** Spilled text lies inside its row's text; long text ≤ its limit,
  short ≤ its limit + 128, a cluster ≤ 128 B.
- **14** Every tabled style, on the screen and in history, is in its
  grid's table.
- **15** `places` matches `table` one to one; ≤ 64 styles share a place.
- **16** The table holds ≤ `MAX_STYLES`.
- **17** History's cell and text counts equal a recount.
- **18** Every block a history row names is held, and its cells fit it.
- **19** Text and link flags match the maps; the side store of rows not
  single width names rows kept, in order.
- **20** Row versions ≤ the grid's version; structural ≤ version.
- **20a** Row ids are unique across history and both screens, never
  reused, and `row_index` finds exactly the row with that id.
- **21** After a trim: bytes ≤ the budget, rows ≤ `MAX_ROWS`, cells ≤
  `MAX_CELLS`; the ring has room for ≤ two chunks more than it keeps,
  each map for ≤ 4 × its length + 64.
- **21a** History is charged the style table's room past the screen's
  share.
- **23** `held` covers the live links' cost, and `held` ≤ `LINK_BYTES`.
- **24** Each row's link runs lie inside its width, sorted and apart;
  every link with an id is in the id map; URIs ≤ 2,083 B, ids ≤ 250 B.
- **25** The cursor and saved cursors are on the grid, within their
  line's columns (half of them on a line not single width).
- **26** Margins lie inside the grid, each the whole of it or `first <
  last`; a row not single width has its flag set.
- **27** Pending wrap is set only by one of its setters. (Not checked:
  each setter's tests cover it.)
- **28** After any operation, every screen position whose row id, cells,
  wrap, links or prompt mark changed is newer than the mark
  (`Row::changed_since`) or `full_refresh_since` says so, and the
  screen's mark moved on; `set_prompt` gives its row a new version, so
  prompt marks count. (Tests: the fuzz targets' `common.rs`; prompt
  marks, which no host reads, `screen::tests::prompts`.)
- **28a** `continues` on history's newest row continues into screen row 0.
  (Tests.)
- **29** Whole and chunked feeding agree on modes, cursor shape,
  margins, palette, pictures and the inactive screen. (Tests:
  `tests/corpus/invariants.rs`.)
- **30** ≤ 64 placements on both screens together.
- **31** Kept pictures + the decoder + its transient ≤ the budget.
- **32** A shown picture's cells hold `PLACEHOLDER`, inside its area, on
  its screen (but rows cropped off a top that keeps no history); its
  rows still exist.
- **35** The key decoder holds ≤ 258 B, a paste's room ≤ limit + 5;
  split and whole input decode alike (tests).
- **36** Kitty stacks ≤ 32 entries; tab stops kept for no more columns
  than a grid has had (≤ 65,536).
- **37** A rejected `resize` leaves every reader-visible field as it
  was, synchronized output included, and the cell printed last, which
  the next character can still join. (Tests: B37.)
