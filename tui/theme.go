package tui

// The TUI's colors (Spec mc-d1pza, ADR 0386): one typed palette names every
// semantic role, and applyTheme reassigns the package's styles from it, so a
// theme switch is one call and the whole TUI repaints. Bubbletea runs Update and
// View on one goroutine, so applyTheme inside Update does not race rendering.

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/lipgloss"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/mdrender"
	"jayulabs.org/mugicrew/session"
)

// pauseInherited keys palette.Pause for a pause a bead only inherits (ADR 0084).
const pauseInherited = "inherited"

// palette is a preset's value for every color role.
type palette struct {
	// chrome
	Accent                         lipgloss.Color
	SelBg, SelInk, SelMarker       lipgloss.Color
	HoverBg, AncestorBg, PickerBg  lipgloss.Color
	ShadeInk                       lipgloss.Color // optional: set, the three shades carry it as their foreground
	HintBg, HintKey, HintDesc      lipgloss.Color
	TabInactiveBorder, TabInactive lipgloss.Color
	TabHoverBorder, TabHover       lipgloss.Color
	Dim, Soft, Warn                lipgloss.Color
	WaitsOnYou, Caution            lipgloss.Color
	FormHint, PickerCheck          lipgloss.Color
	CreateNew                      lipgloss.Color
	RefKey, RefDesc, RefSep        lipgloss.Color
	MDCode, MDLink                 lipgloss.Color
	StatsBars, Burn                lipgloss.Color
	// liveness
	Alive, AFK, BlockerPink lipgloss.Color
	// state
	InProgress  lipgloss.Color
	P0Fg, P1Fg  lipgloss.Color
	ChipInk     lipgloss.Color
	UnknownKind lipgloss.Color
	Pause       map[string]lipgloss.Color // by bd pause cause, plus pauseInherited
	Kind        map[contract.Kind]lipgloss.Color
	Bolder      bool // bold inactive and hovered tabs and reference keys
}

// darkPalette is today's colors, and the default.
var darkPalette = palette{
	Accent: "212",
	SelBg:  "25", SelInk: "15", SelMarker: "111",
	HoverBg: "17", AncestorBg: "236", PickerBg: "237",
	HintBg: "236", HintKey: "252", HintDesc: "245",
	TabInactiveBorder: "240", TabInactive: "245",
	TabHoverBorder: "245", TabHover: "252",
	Dim: "240", Soft: "244", Warn: "203",
	WaitsOnYou: "214", Caution: "214",
	FormHint: "15", PickerCheck: "114", CreateNew: "213",
	RefKey: "241", RefDesc: "238", RefSep: "237",
	MDCode: "180", MDLink: "111",
	StatsBars: "109", Burn: "180",
	Alive: "114", AFK: "151", BlockerPink: "210",
	InProgress: "220",
	P0Fg:       "203", P1Fg: "217",
	ChipInk:     "0",
	UnknownKind: "244",
	Pause: map[string]lipgloss.Color{
		bd.PausedForeignHold: "214", bd.PausedFailing: "203", bd.PausedAbandoned: "208",
		bd.PausedRecovering: "172", bd.PausedCheckup: "179", bd.PausedManual: "213",
		bd.PausedIntegrateConflict: "44", bd.PausedNotBuilt: "220", bd.PausedBehindMainline: "178", bd.PausedUnmet: "167",
		bd.PausedProfile: "75",
		bd.PausedContext: "141", pauseInherited: "127",
	},
	Kind: map[contract.Kind]lipgloss.Color{
		// defog
		contract.KindSignal: "186", contract.KindSurvey: "117", contract.KindGrilling: "202",
		contract.KindResearch: "60", contract.KindPrototype: "170",
		// spine
		contract.KindChart: "68", contract.KindChartConflict: "89", contract.KindSpec: "49",
		contract.KindCheckpoint: "180",
		// execution
		contract.KindBuild: "224", contract.KindReview: "108", contract.KindCritique: "167",
		contract.KindConflict: "88",
		// gates
		contract.KindApproval: "107", contract.KindPivot: "160", contract.KindRollback: "94",
		// hitl misc
		contract.KindErrand: "248", contract.KindDiscussion: "246", contract.KindCheckup: "246", contract.KindRemnant: "160",
		contract.KindSurgery: "93", contract.KindInitiative: "255", contract.KindLanding: "209", contract.KindMRWatch: "209",
		contract.KindForgeReview: "109",
		contract.KindLatch:       "244", // no hue of its own today: it drew in the unknown-kind fallback
	},
}

// lightPalette is for a light terminal background (ADR 0386): dark foregrounds, pale shades with a dark shade ink, no indices 0–15.
var lightPalette = palette{
	Accent:            "162",
	SelBg:             "153",
	SelInk:            "16",
	SelMarker:         "25",
	HoverBg:           "189",
	AncestorBg:        "254",
	PickerBg:          "253",
	ShadeInk:          "235",
	HintBg:            "253",
	HintKey:           "235",
	HintDesc:          "241",
	TabInactiveBorder: "248",
	TabInactive:       "243",
	TabHoverBorder:    "240",
	TabHover:          "235",
	Dim:               "247",
	Soft:              "243",
	Warn:              "160",
	WaitsOnYou:        "166",
	Caution:           "136",
	FormHint:          "236",
	PickerCheck:       "28",
	CreateNew:         "127",
	RefKey:            "238",
	RefDesc:           "243",
	RefSep:            "248",
	MDCode:            "94",
	MDLink:            "25",
	StatsBars:         "30",
	Burn:              "94",
	Alive:             "28",
	AFK:               "66",
	BlockerPink:       "168",
	InProgress:        "136",
	P0Fg:              "160",
	P1Fg:              "131",
	ChipInk:           "231",
	UnknownKind:       "243",
	Pause: map[string]lipgloss.Color{
		bd.PausedForeignHold:       "166",
		bd.PausedFailing:           "160",
		bd.PausedAbandoned:         "202",
		bd.PausedRecovering:        "130",
		bd.PausedCheckup:           "137",
		bd.PausedManual:            "163",
		bd.PausedIntegrateConflict: "30",
		bd.PausedNotBuilt:          "136",
		bd.PausedBehindMainline:    "142",
		bd.PausedUnmet:             "131",
		bd.PausedProfile:           "26",
		bd.PausedContext:           "91",
		pauseInherited:             "139",
	},
	Kind: map[contract.Kind]lipgloss.Color{
		contract.KindSignal:        "100",
		contract.KindSurvey:        "31",
		contract.KindGrilling:      "166",
		contract.KindResearch:      "60",
		contract.KindPrototype:     "127",
		contract.KindChart:         "25",
		contract.KindChartConflict: "89",
		contract.KindSpec:          "29",
		contract.KindCheckpoint:    "137",
		contract.KindBuild:         "138",
		contract.KindReview:        "65",
		contract.KindCritique:      "167",
		contract.KindConflict:      "88",
		contract.KindApproval:      "64",
		contract.KindPivot:         "160",
		contract.KindRollback:      "94",
		contract.KindErrand:        "242",
		contract.KindDiscussion:    "244",
		contract.KindCheckup:       "244",
		contract.KindRemnant:       "160",
		contract.KindSurgery:       "55",
		contract.KindInitiative:    "235",
		contract.KindLanding:       "130",
		contract.KindMRWatch:       "130",
		contract.KindForgeReview:   "66",
		contract.KindLatch:         "243",
	},
}

// highContrastPalette is brighter and bolder (ADR 0386): no gray under 248, a saturated selection bar, bold inactive and hovered tabs and reference keys.
var highContrastPalette = palette{
	Accent:            "207",
	SelBg:             "27",
	SelInk:            "231",
	SelMarker:         "123",
	HoverBg:           "18",
	AncestorBg:        "238",
	PickerBg:          "239",
	HintBg:            "235",
	HintKey:           "231",
	HintDesc:          "252",
	TabInactiveBorder: "248",
	TabInactive:       "252",
	TabHoverBorder:    "252",
	TabHover:          "231",
	Dim:               "248",
	Soft:              "251",
	Warn:              "196",
	WaitsOnYou:        "214",
	Caution:           "221",
	FormHint:          "231",
	PickerCheck:       "46",
	CreateNew:         "207",
	RefKey:            "252",
	RefDesc:           "250",
	RefSep:            "248",
	MDCode:            "222",
	MDLink:            "117",
	StatsBars:         "87",
	Burn:              "222",
	Alive:             "46",
	AFK:               "158",
	BlockerPink:       "204",
	InProgress:        "226",
	P0Fg:              "196",
	P1Fg:              "217",
	ChipInk:           "16",
	UnknownKind:       "251",
	Pause: map[string]lipgloss.Color{
		bd.PausedForeignHold:       "214",
		bd.PausedFailing:           "196",
		bd.PausedAbandoned:         "202",
		bd.PausedRecovering:        "178",
		bd.PausedCheckup:           "222",
		bd.PausedManual:            "201",
		bd.PausedIntegrateConflict: "51",
		bd.PausedNotBuilt:          "226",
		bd.PausedBehindMainline:    "227",
		bd.PausedUnmet:             "209",
		bd.PausedProfile:           "81",
		bd.PausedContext:           "177",
		pauseInherited:             "170",
	},
	Kind: map[contract.Kind]lipgloss.Color{
		contract.KindSignal:        "228",
		contract.KindSurvey:        "123",
		contract.KindGrilling:      "208",
		contract.KindResearch:      "105",
		contract.KindPrototype:     "207",
		contract.KindChart:         "75",
		contract.KindChartConflict: "163",
		contract.KindSpec:          "48",
		contract.KindCheckpoint:    "223",
		contract.KindBuild:         "230",
		contract.KindReview:        "120",
		contract.KindCritique:      "203",
		contract.KindConflict:      "197",
		contract.KindApproval:      "154",
		contract.KindPivot:         "196",
		contract.KindRollback:      "178",
		contract.KindErrand:        "252",
		contract.KindDiscussion:    "250",
		contract.KindCheckup:       "250",
		contract.KindRemnant:       "196",
		contract.KindSurgery:       "141",
		contract.KindInitiative:    "231",
		contract.KindLanding:       "215",
		contract.KindMRWatch:       "215",
		contract.KindForgeReview:   "152",
		contract.KindLatch:         "251",
	},
	Bolder: true,
}

func init() { applyTheme(darkPalette) }

// palettes maps each loop.ThemeNames preset to its palette.
var palettes = map[string]palette{"dark": darkPalette, "light": lightPalette, "high-contrast": highContrastPalette}

// ApplyThemeName installs the named preset's palette, and an unknown name leaves
// the current one. main uses it for the one-shot bead-detail process.
func ApplyThemeName(name string) {
	if p, ok := palettes[name]; ok {
		applyTheme(p)
	}
}

// shade is a row-wide background style, with the palette's shade ink as its
// foreground when it sets one.
func shade(bg, ink lipgloss.Color) lipgloss.Style {
	st := lipgloss.NewStyle().Background(bg)
	if ink != "" {
		st = st.Foreground(ink)
	}
	return st
}

func fgSt(c lipgloss.Color) lipgloss.Style { return lipgloss.NewStyle().Foreground(c) }

// applyTheme reassigns every style var, table and bubbles style from p, in
// dependency order: bases, derived copies, tables, help styles, then mdrender.
func applyTheme(p palette) {
	// bases
	accent, hintBg, dim = p.Accent, p.HintBg, p.Dim
	dimSt, warnSt, aliveSt, afkSt = fgSt(dim), fgSt(p.Warn), fgSt(p.Alive), fgSt(p.AFK)
	waitSt, softSt, pinkFg = fgSt(p.WaitsOnYou), fgSt(p.Soft), fgSt(p.BlockerPink)
	selRow = lipgloss.NewStyle().Background(p.SelBg).Foreground(p.SelInk).Bold(true)
	pickRow, ancRow, hoverRow = shade(p.PickerBg, p.ShadeInk), shade(p.AncestorBg, p.ShadeInk), shade(p.HoverBg, p.ShadeInk)
	kindFallback, chipInk = p.UnknownKind, p.ChipInk
	inProgSty = fgSt(p.InProgress)

	// derived copies
	brand = lipgloss.NewStyle().Bold(true).Foreground(accent)
	badgeSt = lipgloss.NewStyle().Bold(true).Foreground(p.WaitsOnYou)
	hintBar = lipgloss.NewStyle().Background(hintBg)
	refBox = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(accent).Padding(0, 1)
	tabActive = lipgloss.NewStyle().Border(boxActive).BorderForeground(accent).Foreground(accent).Bold(true).Padding(0, 1)
	tabInactive = lipgloss.NewStyle().Border(boxInactive).BorderForeground(p.TabInactiveBorder).Foreground(p.TabInactive).Bold(p.Bolder).Padding(0, 1)
	tabHover = lipgloss.NewStyle().Border(boxInactive).BorderForeground(p.TabHoverBorder).Foreground(p.TabHover).Bold(p.Bolder).Padding(0, 1)
	closedSty = lipgloss.NewStyle().Foreground(dim).Strikethrough(true)
	selMarker = lipgloss.NewStyle().Foreground(p.SelMarker).Bold(true)
	selSt = selMarker
	sectionSt = softSt.Bold(true)
	labelSt = softSt
	deadSty = warnSt.Bold(true)
	blockedSty = deadSty
	blockerMk = pinkFg.Bold(true)
	prio0Sty = fgSt(p.P0Fg).Bold(true)
	prio1Sty = fgSt(p.P1Fg)
	usageWarnSty, usageErrSty = fgSt(p.Caution), warnSt
	tealSt = fgSt(p.StatsBars)
	burnSty = fgSt(p.Burn)
	sgPickSt = fgSt(p.PickerCheck).Bold(true)
	sgStandSt = fgSt(p.CreateNew)
	sgHintSt = lipgloss.NewStyle().Italic(true).Foreground(p.FormHint)

	// tables
	pauseSty = make(map[string]lipgloss.Style, len(p.Pause))
	for cause, c := range p.Pause {
		if cause != pauseInherited {
			pauseSty[cause] = fgSt(c)
		}
	}
	inheritSty = fgSt(p.Pause[pauseInherited])
	kindColors = make(map[contract.Kind]lipgloss.Color, len(p.Kind))
	for k, c := range p.Kind {
		kindColors[k] = c
	}
	sessionLooks = map[session.State]sessionLook{
		session.Alive:  {aliveSt, "●", "alive"},
		session.Exited: {deadSty, "✕", "exited"},
	}
	doneLook = sessionLook{aliveSt.Bold(true), "●", "done — /exit"}
	waitLooks = map[session.Waiting]sessionLook{
		session.WaitTurn: {waitSt, "●", "your turn"},
		session.WaitAsk:  {waitSt, "●", "needs approval"},
	}

	// bubbles help styles: the bar's background must survive each key's style reset
	h := help.New()
	hs := &h.Styles
	hs.ShortKey = hs.ShortKey.Background(hintBg).Foreground(p.HintKey).Bold(true)
	hs.ShortDesc = hs.ShortDesc.Background(hintBg).Foreground(p.HintDesc)
	hs.ShortSeparator = hs.ShortSeparator.Background(hintBg).Foreground(dim)
	hs.FullKey = hs.FullKey.Foreground(p.RefKey).Bold(p.Bolder)
	hs.FullDesc = hs.FullDesc.Foreground(p.RefDesc)
	hs.FullSeparator = hs.FullSeparator.Foreground(p.RefSep)
	hs.Ellipsis = hs.Ellipsis.Foreground(p.RefSep)
	hintHelp = h

	mdrender.SetPalette(mdrender.Palette{Accent: string(accent), Muted: string(p.Soft), Code: string(p.MDCode), Link: string(p.MDLink)})
}
