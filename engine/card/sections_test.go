package card

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func fixture(t *testing.T, name string) *Data {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	d, err := FromScryfallJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestLoyaltyRows(t *testing.T) {
	jace := fixture(t, "jace_the_mind_sculptor")
	rows := Rows(SplitLoyalty, jace.OracleText)
	var costs, keys []string
	for _, r := range rows {
		costs = append(costs, r.Loyalty)
		keys = append(keys, ValueKey(FieldLoyalty, r.Loyalty))
	}
	if want := []string{"+2", "0", "−1", "−12"}; !reflect.DeepEqual(costs, want) {
		t.Errorf("costs = %q, want %q", costs, want)
	}
	if want := []string{"plus", "zero", "minus", "minus"}; !reflect.DeepEqual(keys, want) {
		t.Errorf("keys = %q, want %q", keys, want)
	}
	if rows[2].Text != "Return target creature to its owner's hand." {
		t.Errorf("row 2 text = %q", rows[2].Text)
	}
	if n := RowCount(jace); n != 4 {
		t.Errorf("RowCount = %d, want 4", n)
	}
}

func TestLoyaltyRowsKeepAStaticAbilityWithoutACost(t *testing.T) {
	tibalt := fixture(t, "valki_god_of_lies").Face(1)
	rows := Rows(SplitFor(tibalt), tibalt.OracleText)
	if len(rows) != 4 {
		t.Fatalf("rows = %d, want 4: %+v", len(rows), rows)
	}
	if rows[0].Has(FieldLoyalty) || rows[0].Text[:15] != "As Tibalt enter" {
		t.Errorf("row 0 = %+v, want the static ability with no cost", rows[0])
	}
	if rows[3].Loyalty != "−8" || rows[3].Text != "Exile all graveyards. Add {R}{R}{R}." {
		t.Errorf("row 3 = %+v", rows[3])
	}
	if tibalt.Loyalty != "5" || TextFor("loyalty", tibalt) != "5" {
		t.Errorf("loyalty = %q", tibalt.Loyalty)
	}
}

func TestChapterRows(t *testing.T) {
	saga := fixture(t, "the_eldest_reborn")
	if got := TextFor("description", saga); got != "(As this Saga enters and after your draw step, add a lore counter. Sacrifice after III.)" {
		t.Errorf("description = %q", got)
	}
	rows := Rows(SplitChapters, SectionText(saga, true))
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3", len(rows))
	}
	for i, want := range []string{"I", "II", "III"} {
		if !reflect.DeepEqual(rows[i].Chapters, []string{want}) {
			t.Errorf("row %d chapters = %q, want %q", i, rows[i].Chapters, want)
		}
	}
	if rows[1].Text != "Each opponent discards a card." {
		t.Errorf("row 1 text = %q", rows[1].Text)
	}
	if n := RowCount(saga); n != 3 {
		t.Errorf("RowCount = %d, want 3", n)
	}
	// Without its own box the description opens the rows, with no chapter
	all := Rows(SplitChapters, SectionText(saga, false))
	if len(all) != 4 || all[0].Has(FieldChapter) {
		t.Errorf("rows with the description = %+v", all)
	}
}

func TestChapterRowsJoinChaptersAndCarryOnLines(t *testing.T) {
	rows := Rows(SplitChapters, "I, II — Draw a card.\nThen discard a card.\nIII — Win.")
	if len(rows) != 2 {
		t.Fatalf("rows = %+v", rows)
	}
	if !reflect.DeepEqual(rows[0].Values(FieldChapter), []string{"I", "II"}) || rows[0].Text != "Draw a card.\nThen discard a card." {
		t.Errorf("row 0 = %+v", rows[0])
	}
}

func TestLevelRows(t *testing.T) {
	class := fixture(t, "wizard_class")
	if got := Description(class); got != "(Gain the next level as a sorcery to add its ability.)" {
		t.Errorf("description = %q", got)
	}
	rows := Rows(SplitLevels, SectionText(class, true))
	want := []Row{
		{Text: "You have no maximum hand size."},
		{Mana: "{2}{U}", Level: "2", Text: "When this Class becomes level 2, draw two cards."},
		{Mana: "{4}{U}", Level: "3", Text: "Whenever you draw a card, put a +1/+1 counter on target creature you control."},
	}
	if !reflect.DeepEqual(rows, want) {
		t.Errorf("rows = %+v\nwant %+v", rows, want)
	}
}

func TestLeveler(t *testing.T) {
	student := fixture(t, "student_of_warfare")
	l, ok := ParseLeveler(student.OracleText)
	if !ok {
		t.Fatal("no leveler parsed")
	}
	want := Leveler{
		Up: "Level up {W} ({W}: Put a level counter on this. Level up only as a sorcery.)",
		Levels: []LevelerTier{
			{Range: "2-6", PT: "3/3", Text: "First strike"},
			{Range: "7+", PT: "4/4", Text: "Double strike"},
		},
	}
	if !reflect.DeepEqual(l, want) {
		t.Errorf("leveler = %+v\nwant %+v", l, want)
	}
	for name, want := range map[string]string{
		"level_up": want.Up, "level_label": "LEVEL", "level_1": "2-6", "level_1_pt": "3/3",
		"level_2_text": "Double strike", "level_3": "", "level_9": "",
	} {
		if got := TextFor(name, student); got != want {
			t.Errorf("TextFor(%q) = %q, want %q", name, got, want)
		}
	}
	if got := Rules(student, Taken{Leveler: true}); got != "" {
		t.Errorf("rules with the leveler boxes = %q, want none", got)
	}
	if got := TextFor("level_up", fixture(t, "lightning_bolt")); got != "" {
		t.Errorf("level_up of a non leveler = %q", got)
	}
}

func TestMutate(t *testing.T) {
	gemrazer := fixture(t, "gemrazer")
	if got := TextFor("mutate", gemrazer); got[:16] != "Mutate {1}{G}{G}" {
		t.Errorf("mutate = %q", got)
	}
	if got := Rules(gemrazer, Taken{Mutate: true}); got != "Reach, trample\nWhenever this creature mutates, destroy target artifact or enchantment an opponent controls." {
		t.Errorf("rules = %q", got)
	}
	if got := Rules(gemrazer, Taken{}); got != gemrazer.OracleText {
		t.Errorf("rules with nothing taken = %q", got)
	}
}

func TestPrototype(t *testing.T) {
	proxy := fixture(t, "arcane_proxy")
	for name, want := range map[string]string{
		"prototype_mana": "{1}{U}{U}",
		"prototype_pt":   "2/1",
		"prototype":      "Prototype (You may cast this spell with different mana cost, color, and size. It keeps its abilities and types.)",
	} {
		if got := TextFor(name, proxy); got != want {
			t.Errorf("TextFor(%q) = %q, want %q", name, got, want)
		}
	}
	if got := Rules(proxy, Taken{Prototype: true}); got[:30] != "When this creature enters, if " {
		t.Errorf("rules = %q", got)
	}
}

func TestChaos(t *testing.T) {
	tazeem := fixture(t, "tazeem")
	if got := TextFor("chaos", tazeem); got != "Whenever chaos ensues, draw a card for each land you control." {
		t.Errorf("chaos = %q", got)
	}
	if got := Rules(tazeem, Taken{Chaos: true}); got != "Creatures can't block." {
		t.Errorf("rules = %q", got)
	}
	phenomenon := &Data{TypeLine: "Phenomenon", OracleText: "When you encounter this, each player draws.\nThen chaos ensues."}
	if got := ChaosText(phenomenon); got != "" {
		t.Errorf("a phenomenon's chaos = %q, want none", got)
	}
}

func TestDefense(t *testing.T) {
	battle := fixture(t, "invasion_of_ergamon")
	if battle.Defense != "5" || TextFor("defense", battle) != "5" {
		t.Errorf("defense = %q", battle.Defense)
	}
	if got := TextFor("defense", battle.Face(1)); got != "" {
		t.Errorf("back face defense = %q, want none", got)
	}
}

func TestFlipsideTypeAndMana(t *testing.T) {
	valki := fixture(t, "valki_god_of_lies")
	// Tibalt, Cosmic Impostor is "Legendary Planeswalker — Tibalt", a single
	// subtype, which flipside_type prefers over the supertype
	if got := TextFor("flipside_type", valki); got != "Tibalt" {
		t.Errorf("front flipside_type = %q", got)
	}
	if got := TextFor("flipside_mana", valki); got != "{5}{B}{R}" {
		t.Errorf("front flipside_mana = %q", got)
	}
	// Valki, God of Lies is "Legendary Creature — God", likewise a single
	// subtype
	if got := TextFor("flipside_type", valki.Face(1)); got != "God" {
		t.Errorf("back flipside_type = %q", got)
	}
	land := &Data{Layout: "modal_dfc", Faces: []Face{{TypeLine: "Instant"}, {TypeLine: "Land", OracleText: "As this enters, you may pay 3 life.\n{T}: Add {B}. Then scry 1."}}}
	if got := TextFor("flipside_mana", land); got != "{T}: Add {B}." {
		t.Errorf("land flipside_mana = %q", got)
	}
	if got := TextFor("flipside_type", fixture(t, "delver")); got != "" {
		t.Errorf("a transform card's flipside_type = %q, want none", got)
	}
	// A face with no subtype, or more than one, falls back to its last
	// supertype or type instead of guessing which subtype to print
	noSub := &Data{Layout: "modal_dfc", Faces: []Face{{TypeLine: "Instant"}, {TypeLine: "Sorcery"}}}
	if got := TextFor("flipside_type", noSub); got != "Sorcery" {
		t.Errorf("no-subtype flipside_type = %q", got)
	}
	twoSub := &Data{Layout: "modal_dfc", Faces: []Face{{TypeLine: "Instant"}, {TypeLine: "Creature — Elf Warrior"}}}
	if got := TextFor("flipside_type", twoSub); got != "Creature" {
		t.Errorf("two-subtype flipside_type = %q", got)
	}
}

func TestTakingNothingLeavesTheRulesAsPrinted(t *testing.T) {
	for _, name := range []string{"jace_the_mind_sculptor", "the_eldest_reborn", "wizard_class", "student_of_warfare", "gemrazer", "arcane_proxy", "tazeem", "lightning_bolt"} {
		d := fixture(t, name)
		if got := Rules(d, Taken{}); got != d.OracleText {
			t.Errorf("%s: rules = %q", name, got)
		}
	}
}

func TestEveryBoxNameIsKnown(t *testing.T) {
	var take Taken
	for _, name := range []string{"description", "mutate", "prototype_pt", "chaos", "level_1_text"} {
		if !take.Take(name) {
			t.Errorf("%q takes nothing", name)
		}
	}
	if take.Take("title") {
		t.Error("title takes something")
	}
	names := map[string]bool{}
	for _, n := range TextBoxNames() {
		names[n] = true
	}
	for _, n := range []string{"level_up", "level_2_pt", "prototype_mana", "chaos", "flipside_type", "loyalty", "defense"} {
		if !names[n] {
			t.Errorf("TextBoxNames lacks %q", n)
		}
	}
}

// TestFlipsideTypeOfACreatureFaceIsItsSubtype covers the rule a card printed in
// Modern Horizons 3 or earlier follows, which every card follows until the set a
// card comes from reaches this (#138). A creature face with one subtype prints
// the subtype, where a layout written before this printed "Creature"
func TestFlipsideTypeOfACreatureFaceIsItsSubtype(t *testing.T) {
	for _, tc := range []struct {
		line, want string
	}{
		{"Creature — Bird", "Bird"},
		{"Legendary Creature — God", "God"},
		{"Artifact Creature — Construct", "Construct"},
		{"Creature — Human Soldier", "Creature"},
	} {
		d := &Data{Layout: "modal_dfc", Faces: []Face{{TypeLine: "Instant"}, {TypeLine: tc.line}}}
		if got := TextFor("flipside_type", d); got != tc.want {
			t.Errorf("flipside of %q = %q, want %q", tc.line, got, tc.want)
		}
	}
}
