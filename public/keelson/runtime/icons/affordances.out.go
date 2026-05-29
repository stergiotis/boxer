package icons

// Slot A — UI affordances (33 constants). Curated alias layer over the
// generated Phosphor catalogue (phosphor.out.go). Call sites reach for
// these `IconXxx` names rather than the underlying `Ph*` constants
// because the curated names are conceptual and survive Phosphor renames
// — see ADR-0044 §SD3.
//
// Drift notes (from the M0 survey against the legacy nerdfont package
// — cf. iconset-comparison.html) live inline next to each constant.
// ADR-0044 §SD4 records the family-wide outline-vs-filled and
// density-weight decisions deferred to M2.

const (
	// IconCheck (ph:check) — affirm, complete, success.
	IconCheck = PhCheck

	// IconCircle (ph:circle) — outline circle. Replaces the legacy
	// CodCircleFilled status-dot usage; ADR-0044 §SD4 defers
	// Phosphor-Fill.ttf embedding (which would supply a true filled
	// dot) to M2.
	IconCircle = PhCircle

	// IconClose (ph:x) — close, dismiss, cancel.
	IconClose = PhX

	// IconColorMode (ph:circle-half) — light/dark mode toggle.
	IconColorMode = PhCircleHalf

	// IconDatabase (ph:database) — generic database. Note: previously
	// also reached via nf.DevDatabase, which ADR-0044 reclassifies as
	// an affordance (no Devicon brand mark exists for "database").
	IconDatabase = PhDatabase

	// IconPause (ph:pause) — media / debug pause.
	IconPause = PhPause

	// IconStop (ph:stop) — media / debug stop.
	IconStop = PhStop

	// IconError (ph:x-circle) — error indicator. Codicons used a filled
	// circle with X cut out; the Phosphor outline equivalent reads as
	// the same shape semantically.
	IconError = PhXCircle

	// IconFile (ph:file) — generic file.
	IconFile = PhFile

	// IconFileImage (ph:file-image) — image / media file. Drift: legacy
	// CodFileMedia covered image + video + audio; this constant narrows
	// to image.
	IconFileImage = PhFileImage

	// IconFolder (ph:folder) — folder closed.
	IconFolder = PhFolder

	// IconFolderOpen (ph:folder-open) — folder open.
	IconFolderOpen = PhFolderOpen

	// IconGear (ph:gear) — gear. Use for actions framed as "settings"
	// where the gear metaphor reads better than sliders.
	IconGear = PhGear

	// IconGitMerge (ph:git-merge) — git merge action. Generic git
	// affordance, not a Git brand mark.
	IconGitMerge = PhGitMerge

	// IconGitBranch (ph:git-branch) — git branch action. Replaces
	// nf.DevGitBranch which ADR-0044 reclassifies as an affordance
	// (the glyph is the universal branch-fork shape, not the Git logo).
	IconGitBranch = PhGitBranch

	// IconGlobe (ph:globe) — globe / world / network.
	IconGlobe = PhGlobe

	// IconChartBar (ph:chart-bar) — bar chart. Replaces nf.CodGraph
	// (Codicons named the bar-chart glyph "graph").
	IconChartBar = PhChartBar

	// IconChartLine (ph:chart-line) — line chart.
	IconChartLine = PhChartLine

	// IconInfo (ph:info) — info indicator.
	IconInfo = PhInfo

	// IconBracketsCurly (ph:brackets-curly) — { } pair, used for JSON.
	// Drift: Codicons renders the literal characters "JSON"; the
	// brackets glyph is the modern UI convention.
	IconBracketsCurly = PhBracketsCurly

	// IconMap (ph:map-trifold) — folded-paper map.
	IconMap = PhMapTrifold

	// IconPaintBucket (ph:paint-bucket) — paint bucket.
	IconPaintBucket = PhPaintBucket

	// IconPlay (ph:play) — media / debug play. Outline; ADR-0044 §SD4
	// records the filled-variant deferral.
	IconPlay = PhPlay

	// IconSave (ph:floppy-disk) — save (floppy convention).
	IconSave = PhFloppyDisk

	// IconSaveAs (ph:floppy-disk-back) — save-as.
	IconSaveAs = PhFloppyDiskBack

	// IconSearch (ph:magnifying-glass) — search.
	IconSearch = PhMagnifyingGlass

	// IconSliders (ph:sliders-horizontal) — sliders/preferences.
	// Replaces nf.CodSettings (Codicons named the sliders glyph
	// "settings", distinct from "gear").
	IconSliders = PhSlidersHorizontal

	// IconLightning (ph:lightning) — events, triggers, energy. Replaces
	// nf.CodSymbolEvent.
	IconLightning = PhLightning

	// IconTreeStructure (ph:tree-structure) — hierarchical structure.
	// Replaces nf.CodSymbolStructure.
	IconTreeStructure = PhTreeStructure

	// IconTable (ph:table) — table / grid.
	IconTable = PhTable

	// IconTag (ph:tag) — tag / label.
	IconTag = PhTag

	// IconWarning (ph:warning) — warning triangle.
	IconWarning = PhWarning

	// IconPalette (ph:palette) — color palette. Replaces nf.FaPalette.
	IconPalette = PhPalette

	// IconWaveform (ph:waveform) — audio / signal waveform. Replaces
	// nf.MdWaveform.
	IconWaveform = PhWaveform
)
