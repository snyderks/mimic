package card

import (
	"regexp"
	"slices"
	"strings"
)

// Row is one ability, chapter, or level a card's rules text splits into, for a
// template that lays each out as a row of its own
type Row struct {
	// Loyalty is a loyalty ability's cost, such as "+1" or "−3"
	Loyalty string
	// Mana is the mana a class level costs
	Mana string
	// Chapters are the saga chapters the row is for, as printed, such as "I" and
	// "II" for a row the card prints as "I, II —"
	Chapters []string
	// Level is a class level's number
	Level string
	// Text is the row's rules text
	Text string
}

// The ways a section splits rules text into rows
const (
	// SplitLoyalty is a planeswalker's loyalty abilities, a row for each line
	SplitLoyalty = "loyalty"
	// SplitChapters is a saga's chapters, a row for each chapter line
	SplitChapters = "chapters"
	// SplitLevels is a class's levels, a row for each level and its abilities
	SplitLevels = "levels"
)

// SplitNames lists the ways a section can split rules text, sorted
func SplitNames() []string { return []string{SplitChapters, SplitLevels, SplitLoyalty} }

// The fields of a row a template can draw beside it or pick an image by
const (
	FieldLoyalty = "loyalty"
	FieldMana    = "mana"
	FieldChapter = "chapter"
	FieldLevel   = "level"
)

// RowFields lists the fields of a row, sorted
func RowFields() []string { return []string{FieldChapter, FieldLevel, FieldLoyalty, FieldMana} }

// Values are what the row holds for a field: one value, several for the
// chapters of a row that covers more than one, or none when the row has none
func (r Row) Values(field string) []string {
	switch field {
	case FieldLoyalty:
		if r.Loyalty != "" {
			return []string{r.Loyalty}
		}
	case FieldMana:
		if r.Mana != "" {
			return []string{r.Mana}
		}
	case FieldChapter:
		return r.Chapters
	case FieldLevel:
		if r.Level != "" {
			return []string{r.Level}
		}
	}
	return nil
}

// Has reports whether the row holds anything for a field
func (r Row) Has(field string) bool { return len(r.Values(field)) > 0 }

// ValueKey is the name a template picks a row's image by for one value of a
// field. A loyalty cost is keyed by its sign, "plus", "minus", or "zero", since
// a planeswalker's badges differ by sign and not by number. Any other value is
// its own key
func ValueKey(field, value string) string {
	if field != FieldLoyalty || !loyaltyCostRe.MatchString(value) {
		return value
	}
	switch {
	case strings.HasPrefix(value, "+"):
		return "plus"
	case strings.HasPrefix(value, "-"), strings.HasPrefix(value, "−"):
		return "minus"
	}
	return "zero"
}

// ValueKeys lists the keys a field's values can take that are not the values
// themselves, for a check of a template's image names
func ValueKeys(field string) []string {
	switch field {
	case FieldLoyalty:
		return []string{"minus", "plus", "zero"}
	case FieldChapter:
		return []string{"I", "II", "III", "IV", "V", "VI", "VII"}
	}
	return nil
}

var (
	loyaltyCostRe = regexp.MustCompile(`^[+\-−]?(?:\d+|X)$`)
	chapterRe     = regexp.MustCompile(`^([IVX]+(?:, [IVX]+)*) — (.*)$`)
	classLevelRe  = regexp.MustCompile(`^(.+?): Level (\d+)$`)
)

// Rows splits rules text into rows the way split names. Text that the split
// does not recognize, such as a static ability on a planeswalker or the lines
// before a saga's first chapter, becomes a row with no key field of its own. An
// unknown split returns no rows
func Rows(split, text string) []Row {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	lines := strings.Split(text, "\n")
	switch split {
	case SplitLoyalty:
		return loyaltyRows(lines)
	case SplitChapters:
		return chapterRows(lines)
	case SplitLevels:
		return levelRows(lines)
	}
	return nil
}

// loyaltyRows reads each line as an ability, with a cost when it opens with one
// followed by a colon
func loyaltyRows(lines []string) []Row {
	rows := make([]Row, 0, len(lines))
	for _, line := range lines {
		cost, text, ok := strings.Cut(line, ": ")
		if ok && loyaltyCostRe.MatchString(cost) {
			rows = append(rows, Row{Loyalty: cost, Text: text})
			continue
		}
		rows = append(rows, Row{Text: line})
	}
	return rows
}

// chapterRows starts a row at each chapter line and adds any other line to the
// row before it, or to a row of its own when no chapter has come yet
func chapterRows(lines []string) []Row {
	var rows []Row
	for _, line := range lines {
		if m := chapterRe.FindStringSubmatch(line); m != nil {
			rows = append(rows, Row{Chapters: strings.Split(m[1], ", "), Text: m[2]})
			continue
		}
		rows = appendLine(rows, Row{Text: line}, line)
	}
	return rows
}

// levelRows starts a row at each level line, which carries the level's mana
// cost, and reads the lines after it as that level's abilities. The lines before
// the first are the abilities a class starts with, a row with no mana or level
// since the card prints neither for it
func levelRows(lines []string) []Row {
	var rows []Row
	for _, line := range lines {
		if m := classLevelRe.FindStringSubmatch(line); m != nil {
			rows = append(rows, Row{Mana: m[1], Level: m[2]})
			continue
		}
		rows = appendLine(rows, Row{Text: line}, line)
	}
	return rows
}

// appendLine adds a line to the last row, or starts the first row with it
func appendLine(rows []Row, first Row, line string) []Row {
	if len(rows) == 0 {
		return append(rows, first)
	}
	last := &rows[len(rows)-1]
	if last.Text == "" {
		last.Text = line
	} else {
		last.Text += "\n" + line
	}
	return rows
}

// Leveler is a leveler card's rules text: the level up ability and each tier of
// levels after it
type Leveler struct {
	Up     string
	Levels []LevelerTier
}

// LevelerTier is one LEVEL band of a leveler: the levels it covers, such as
// "2-6" or "7+", the power and toughness it sets, and its abilities
type LevelerTier struct {
	Range, PT, Text string
}

var (
	levelerLineRe = regexp.MustCompile(`^LEVEL (\S+)$`)
	ptLineRe      = regexp.MustCompile(`^[\dX*+\-]+/[\dX*+\-]+$`)
)

// ParseLeveler splits a leveler's rules text into its level up ability and its
// tiers, and reports false for text with no LEVEL line
func ParseLeveler(oracle string) (Leveler, bool) {
	var out Leveler
	var up []string
	tier := -1
	for _, line := range strings.Split(oracle, "\n") {
		if m := levelerLineRe.FindStringSubmatch(line); m != nil {
			out.Levels = append(out.Levels, LevelerTier{Range: m[1]})
			tier = len(out.Levels) - 1
			continue
		}
		if tier < 0 {
			up = append(up, line)
			continue
		}
		t := &out.Levels[tier]
		switch {
		case t.PT == "" && t.Text == "" && ptLineRe.MatchString(line):
			t.PT = line
		case t.Text == "":
			t.Text = line
		default:
			t.Text += "\n" + line
		}
	}
	out.Up = strings.Join(up, "\n")
	return out, len(out.Levels) > 0
}

// Prototype is the prototype line of a prototype card: the mana cost and the
// power and toughness the card has when cast for it, and the reminder text
type Prototype struct {
	ManaCost, PT, Reminder string
}

var prototypeRe = regexp.MustCompile(`^Prototype (.+?) [—\-] (\S+/\S+)(?: (\(.*\)))?$`)

// ParsePrototype reads a card's prototype line, the first line of its rules
// text, and reports false for a card that has none
func ParsePrototype(oracle string) (Prototype, bool) {
	first, _, _ := strings.Cut(oracle, "\n")
	m := prototypeRe.FindStringSubmatch(first)
	if m == nil {
		return Prototype{}, false
	}
	return Prototype{ManaCost: m[1], PT: m[2], Reminder: m[3]}, true
}

// Text is what a prototype card prints in its prototype box, the keyword and
// its reminder, since the cost and the power and toughness print apart
func (p Prototype) Text() string {
	if p.Reminder == "" {
		return "Prototype"
	}
	return "Prototype " + p.Reminder
}

// MutateText is a mutate card's mutate ability, the first line of its rules
// text, or empty for a card that has none
func MutateText(oracle string) string {
	first, _, _ := strings.Cut(oracle, "\n")
	if strings.HasPrefix(first, "Mutate ") {
		return first
	}
	return ""
}

// ChaosText is a plane's chaos ability, the last line of its rules text when
// that line is the one that triggers on chaos, or empty for a card that has none
func ChaosText(d *Data) string {
	if !hasType(d.TypeLine, "plane") {
		return ""
	}
	i := strings.LastIndex(d.OracleText, "\n")
	if i < 0 {
		return ""
	}
	last := d.OracleText[i+1:]
	if !strings.Contains(strings.ToLower(last), "chaos") {
		return ""
	}
	return last
}

// Description is the line a saga or a class prints above its rows, the reminder
// of how the card works, or empty for any other card and for one whose first
// line is already a row
func Description(d *Data) string {
	split := SplitFor(d)
	if split != SplitChapters && split != SplitLevels {
		return ""
	}
	first, _, _ := strings.Cut(d.OracleText, "\n")
	if chapterRe.MatchString(first) || classLevelRe.MatchString(first) {
		return ""
	}
	return first
}

// SplitFor is how a face's rules text splits into rows, by its type line:
// loyalty abilities for a planeswalker, chapters for a saga, and levels for a
// class. It is empty for any other face
func SplitFor(d *Data) string {
	types, subtypes, _ := strings.Cut(strings.ToLower(d.TypeLine), "—")
	switch {
	case slices.Contains(strings.Fields(types), "planeswalker"):
		return SplitLoyalty
	case slices.Contains(strings.Fields(subtypes), "saga"):
		return SplitChapters
	case slices.Contains(strings.Fields(subtypes), "class"):
		return SplitLevels
	}
	return ""
}

// RowCount is how many rows a face's rules text splits into for its kind, with
// a saga's or a class's description left out, and a leveler's tiers. It is 0
// for a face whose text does not split
func RowCount(d *Data) int {
	if split := SplitFor(d); split != "" {
		return len(Rows(split, Rules(d, Taken{Description: true})))
	}
	if l, ok := ParseLeveler(d.OracleText); ok {
		return len(l.Levels)
	}
	return 0
}

// hasType reports whether a type line names the card type before its dash
func hasType(typeLine, word string) bool {
	types, _, _ := strings.Cut(strings.ToLower(typeLine), "—")
	return slices.Contains(strings.Fields(types), word)
}

// Taken says which parts of a face's rules text a layout prints in boxes of
// their own, so the box for the rest leaves them out
type Taken struct {
	Description bool
	Mutate      bool
	Prototype   bool
	Chaos       bool
	Leveler     bool
	// Rows is everything a section splits into rows, which is the whole text
	// except the description when that is taken too
	Rows bool
}

// Take marks what a box that draws name takes out of the rules text, and
// reports whether the name takes anything
func (t *Taken) Take(name string) bool {
	switch {
	case name == "description":
		t.Description = true
	case name == "mutate":
		t.Mutate = true
	case strings.HasPrefix(name, "prototype"):
		t.Prototype = true
	case name == "chaos":
		t.Chaos = true
	case strings.HasPrefix(name, "level_"):
		t.Leveler = true
	default:
		return false
	}
	return true
}

// Rules is a face's rules text without what a layout prints apart, the text a
// layout's rules box draws. With nothing taken it is the rules text as printed
func Rules(d *Data, t Taken) string {
	text := d.OracleText
	if t.Leveler {
		if _, ok := ParseLeveler(text); ok {
			return ""
		}
	}
	if t.Description {
		if desc := Description(d); desc != "" {
			text = dropFirstLine(text)
		}
	}
	if t.Rows && SplitFor(d) != "" {
		return ""
	}
	if t.Mutate && MutateText(text) != "" {
		text = dropFirstLine(text)
	}
	if t.Prototype {
		if _, ok := ParsePrototype(text); ok {
			text = dropFirstLine(text)
		}
	}
	if t.Chaos {
		if chaos := ChaosText(d); chaos != "" {
			text = strings.TrimSuffix(strings.TrimSuffix(text, chaos), "\n")
		}
	}
	return text
}

// SectionText is the part of a face's rules text a section splits into rows:
// all of it, or all but the description when a box of its own prints that
func SectionText(d *Data, descriptionTaken bool) string {
	if descriptionTaken && Description(d) != "" {
		return dropFirstLine(d.OracleText)
	}
	return d.OracleText
}

func dropFirstLine(s string) string {
	_, rest, _ := strings.Cut(s, "\n")
	return rest
}

// levelerText is what the leveler boxes print: level_up, and for each tier n
// from 1, level_n with the levels it covers, level_n_pt, and level_n_text, and
// level_label, the word each tier is headed by
func levelerText(name string, d *Data) (string, bool) {
	if !strings.HasPrefix(name, "level_") {
		return "", false
	}
	l, ok := ParseLeveler(d.OracleText)
	if name == "level_up" || name == "level_label" {
		switch {
		case !ok:
			return "", true
		case name == "level_up":
			return l.Up, true
		default:
			return "LEVEL", true
		}
	}
	rest := strings.TrimPrefix(name, "level_")
	num, field, _ := strings.Cut(rest, "_")
	if len(num) != 1 || num[0] < '1' || num[0] > '0'+maxLevelerTiers {
		return "", false
	}
	i := int(num[0] - '1')
	if !ok || i >= len(l.Levels) {
		return "", true
	}
	t := l.Levels[i]
	switch field {
	case "":
		return t.Range, true
	case "pt":
		return t.PT, true
	case "text":
		return t.Text, true
	}
	return "", false
}

// maxLevelerTiers is how many tiers the leveler boxes name. A printed leveler
// has two
const maxLevelerTiers = 3

// levelerBoxNames lists the names of the leveler boxes
func levelerBoxNames() []string {
	out := []string{"level_label", "level_up"}
	for i := 1; i <= maxLevelerTiers; i++ {
		n := "level_" + string(rune('0'+i))
		out = append(out, n, n+"_pt", n+"_text")
	}
	return out
}

// flipsideType is the card type of a modal double-faced card's other face,
// which the face prints at its bottom, such as "Tibalt" or "Sorcery". A card
// printed in Modern Horizons 3 or earlier prints the last type or subtype on
// the other face's type line, which is what this reads: the subtype where the
// face prints exactly one, such as "Bird" or a planeswalker's own name, and the
// last supertype or type otherwise. A card printed after it ignores subtypes,
// which needs the set the card comes from to tell apart (#138). A face that
// prints more than one subtype has no single word to show, so it falls back to
// its type
func flipsideType(d *Data) string {
	o, ok := otherFace(d)
	if !ok {
		return ""
	}
	types, subtypes, hasDash := strings.Cut(o.TypeLine, "—")
	if hasDash {
		if sub := strings.Fields(subtypes); len(sub) == 1 {
			return sub[0]
		}
	}
	words := strings.Fields(types)
	if len(words) == 0 {
		return ""
	}
	return words[len(words)-1]
}

// flipsideMana is what a modal double-faced card's other face costs, printed
// beside its type: its mana cost, or a land's mana ability
func flipsideMana(d *Data) string {
	o, ok := otherFace(d)
	if !ok {
		return ""
	}
	if !hasType(o.TypeLine, "land") {
		return o.ManaCost
	}
	for _, line := range strings.Split(o.OracleText, "\n") {
		if strings.HasPrefix(line, "{T}") {
			first, _, _ := strings.Cut(line, ".")
			return first + "."
		}
	}
	return ""
}

// otherFace is the face of a modal double-faced card that d does not print
func otherFace(d *Data) (Face, bool) {
	if d.Layout != "modal_dfc" || len(d.Faces) != 2 || d.FaceIndex < 0 || d.FaceIndex > 1 {
		return Face{}, false
	}
	return d.Faces[1-d.FaceIndex], true
}
