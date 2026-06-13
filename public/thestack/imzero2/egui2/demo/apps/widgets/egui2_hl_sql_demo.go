package widgets

import (
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/codeview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// Pre-built retained jobs for static SQL — zero per-frame cost on Go side.
var (
	sqlSimple = codeview.PrepareSql(`SELECT name, age FROM users WHERE age > 18 ORDER BY name`)
	sqlJoin   = codeview.PrepareSql(`SELECT o.id, c.name, o.total
FROM orders AS o
JOIN customers AS c ON o.customer_id = c.id
WHERE o.total > 100.0
ORDER BY o.total DESC`)
	sqlCte = codeview.PrepareSql(`WITH monthly_sales AS (
    SELECT
        toStartOfMonth(created_at) AS month,
        sum(amount) AS total,
        count(*) AS num_orders
    FROM sales
    WHERE created_at >= '2024-01-01'
    GROUP BY month
)
SELECT month, total, num_orders
FROM monthly_sales
ORDER BY month`)
	sqlParamSlot = codeview.PrepareSql(`SELECT * FROM events WHERE event_type = {type: String} AND ts >= {start: DateTime}`)
)

func demoInternationalText(ids *c.WidgetIdStack) {
	// Scripts limited to those covered by the default font chain
	// (Noto Sans for Latin/Greek/Cyrillic, NotoSansMonoCJK fallback for
	// CJK). Arabic and Indic/SE-Asian scripts are excluded — they need
	// HarfBuzz-class shaping (joining for Arabic, mark positioning for
	// Devanagari/Thai/Tamil) which epaint 0.34.x does not ship. See
	// doc/explanation/egui-arabic-bidi-status.md for the pipeline
	// analysis and the upstream harfrust integration status.
	for range c.CollapsingHeader(ids.PrepareStr("i18n-mixed"), c.WidgetText().Text("mixed scripts").Keep()).DefaultOpen(true).KeepIter() {
		atoms := c.Atoms()
		for rt := range atoms.StyledText("English ") {
			rt.Strong()
		}
		for rt := range atoms.StyledText("中文 ") { // Chinese
			rt.Strong()
		}
		for rt := range atoms.StyledText("Ελληνικά ") { // Greek
			rt.Italics()
		}
		for rt := range atoms.StyledText("Русский ") { // Russian
			rt.Strong()
		}
		for rt := range atoms.StyledText("日本語 ") { // Japanese
			rt.Italics()
		}
		for rt := range atoms.StyledText("한국어") { // Korean
			rt.Strong()
		}
		c.LabelAtoms(atoms.Keep()).Send()
	}

	for range c.CollapsingHeader(ids.PrepareStr("i18n-cjk"), c.WidgetText().Text("CJK").Keep()).DefaultOpen(true).KeepIter() {
		// Chinese
		for rt := range c.RichTextLabel("天地玄黄，宇宙洪荒。日月盈昃，辰宿列张。") {
			rt.Strong()
		}
		// Japanese
		for rt := range c.RichTextLabel("吾輩は猫である。名前はまだ無い。どこで生れたかとんと見当がつかぬ。") {
			rt.Italics()
		}
		// Korean
		for rt := range c.RichTextLabel("나랏말싸미 듕귁에 달아 문자와로 서르 사맛디 아니할쎄") {
			_ = rt
		}
	}

	for range c.CollapsingHeader(ids.PrepareStr("i18n-european"), c.WidgetText().Text("European scripts").Keep()).DefaultOpen(true).KeepIter() {
		gold := color.Hex(styletokens.AccentDefault.AsHex()).Keep()
		bg := color.Transparent.Keep()

		// Greek
		for rt := range c.RichTextLabelColored(gold, bg, "Μῆνιν ἄειδε, θεά, Πηληϊάδεω Ἀχιλῆος οὐλομένην") {
			rt.Italics()
		}
		// Cyrillic
		for rt := range c.RichTextLabel("Все счастливые семьи похожи друг на друга, каждая несчастливая семья несчастлива по-своему.") {
			_ = rt
		}
	}

	for range c.CollapsingHeader(ids.PrepareStr("i18n-emoji"), c.WidgetText().Text("emoji & symbols").Keep()).KeepIter() {
		atoms := c.Atoms()
		for rt := range atoms.StyledText("Flags: 🇨🇭🇩🇪🇫🇷🇯🇵🇰🇷🇸🇦 ") {
			_ = rt
		}
		for rt := range atoms.StyledText("Math: ∀x∈ℝ: x²≥0 ∧ ∑ⁿᵢ₌₁ ") {
			rt.Monospace()
		}
		for rt := range atoms.StyledText("Misc: ♠♣♥♦ ★☆ ⚡⚙⚛") {
			_ = rt
		}
		c.LabelAtoms(atoms.Keep()).Send()
	}

	// Inline-styling test bed for the SVG exporter — each subline mixes
	// plain text with one styled run so the exported badges-extras /
	// markdown / json palette can be validated against TextFormat fields
	// (italics, underline, strikethrough, background, font_id swaps for
	// Strong / Code).
	for range c.CollapsingHeader(ids.PrepareStr("i18n-styling"), c.WidgetText().Text("inline styling (bold / underline / strike / code / colored bg)").Keep()).DefaultOpen(true).KeepIter() {
		yellow := color.Hex(styletokens.AccentDefault.AsHex()).Keep()
		paleBlue := color.Hex(styletokens.AccentSubtle.AsHex()).Keep()
		zero := color.Transparent.Keep()

		// strong / italic / underline / strikethrough / code in one label,
		// each as its own section so per-section routing is exercised.
		atoms := c.Atoms()
		for rt := range atoms.StyledText("plain ") {
			_ = rt
		}
		for rt := range atoms.StyledText("strong ") {
			rt.Strong()
		}
		for rt := range atoms.StyledText("italic ") {
			rt.Italics()
		}
		for rt := range atoms.StyledText("underline ") {
			rt.Underline()
		}
		for rt := range atoms.StyledText("strikethrough ") {
			rt.Strikethrough()
		}
		for rt := range atoms.StyledText("code") {
			rt.Code()
		}
		c.LabelAtoms(atoms.Keep()).Send()

		// Colored foreground + non-transparent background on one run,
		// surrounded by plain text. Exercises format.background.
		atoms2 := c.Atoms()
		for rt := range atoms2.StyledText("Before… ") {
			_ = rt
		}
		for rt := range atoms2.StyledTextColored(yellow, paleBlue, "highlighted run") {
			_ = rt
		}
		for rt := range atoms2.StyledText(" …after") {
			_ = rt
		}
		c.LabelAtoms(atoms2.Keep()).Send()

		// Combined modifiers in one section to test that a single run
		// can carry multiple TextFormat flags at once.
		atoms3 := c.Atoms()
		for rt := range atoms3.StyledText("Combined: ") {
			_ = rt
		}
		for rt := range atoms3.StyledTextColored(yellow, zero, "bold + italic + underline") {
			rt.Strong().Italics().Underline()
		}
		c.LabelAtoms(atoms3.Keep()).Send()
	}
}

func demoSqlView(ids *c.WidgetIdStack) {
	for range c.CollapsingHeader(ids.PrepareStr("sql-simple"), c.WidgetText().Text("simple query").Keep()).DefaultOpen(true).KeepIter() {
		c.CodeView(ids.PrepareStr("cv-simple"), sqlSimple).Send()
	}

	for range c.CollapsingHeader(ids.PrepareStr("sql-join"), c.WidgetText().Text("join with aliases").Keep()).DefaultOpen(true).KeepIter() {
		c.CodeView(ids.PrepareStr("cv-join"), sqlJoin).Send()
	}

	for range c.CollapsingHeader(ids.PrepareStr("sql-cte"), c.WidgetText().Text("CTE with aggregation").Keep()).DefaultOpen(true).KeepIter() {
		c.CodeView(ids.PrepareStr("cv-cte"), sqlCte).Send()
	}

	for range c.CollapsingHeader(ids.PrepareStr("sql-paramslot"), c.WidgetText().Text("parameter slots").Keep()).KeepIter() {
		c.CodeView(ids.PrepareStr("cv-param"), sqlParamSlot).Send()
	}
}
