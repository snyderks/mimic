package psd

import (
	"strings"
	"testing"
	"unicode/utf16"
)

// engineString writes a UTF16 engine data string with a byte order mark,
// escaping the bytes that need it
func engineString(s string) string {
	var raw []byte
	raw = append(raw, 0xfe, 0xff)
	for _, u := range utf16.Encode([]rune(s)) {
		raw = append(raw, byte(u>>8), byte(u))
	}
	var out strings.Builder
	out.WriteByte('(')
	for _, c := range raw {
		if c == '(' || c == ')' || c == '\\' {
			out.WriteByte('\\')
		}
		out.WriteByte(c)
	}
	out.WriteByte(')')
	return out.String()
}

func engineFixture() []byte {
	return []byte("\n\n<<\n\t/EngineDict\n\t<<\n\t\t/Editor\n\t\t<<\n\t\t\t/Text " + engineString("Hello\rWorld\r") + `
		>>
		/ParagraphRun << /RunArray [ << /ParagraphSheet << /Properties << /Justification 2 >> >> >> ] >>
		/StyleRun
		<<
			/RunArray [
			<< /StyleSheet << /StyleSheetData << /Font 1 /FontSize 12.5 /Tracking 20 /FillColor << /Type 1 /Values [ 1.0 1.0 .0 0.0 ] >> >> >> >>
			<< /StyleSheet << /StyleSheetData << /FauxBold true >> >> >>
			]
			/RunLengthArray [ 6 6 ]
		>>
		/Rendered << /Version 1 /Shapes << /Children [ << /ShapeType 1 /Cookie << /Photoshop << /BoxBounds [ 0.0 -1.5 200.5 50.0 ] >> >> >> ] >> >>
	>>
	/ResourceDict
	<<
		/FontSet [ << /Name ` + engineString("MinionPro") + ` /Type 1 >> << /Name ` + engineString("Beleren)Bold") + ` >> ]
		/StyleSheetSet [ << /Name ` + engineString("Normal") + ` /StyleSheetData << /Font 0 /FontSize 9 /Leading 11 /AutoLeading false /Tracking 0 >> >> ]
		/TheNormalStyleSheet 0
		/ParagraphSheetSet [ << /Properties << /Justification 0 >> >> ]
		/TheNormalParagraphSheet 0
		/Kind /Name
		/Plain (abc)
	>>
>>` + "\x00")
}

func TestParseEngineData(t *testing.T) {
	e, err := ParseEngineData(engineFixture())
	if err != nil {
		t.Fatal(err)
	}
	if s := engineGet(e, "EngineDict", "Editor", "Text"); s != "Hello\rWorld\r" {
		t.Errorf("text %q", s)
	}
	if s := engineGet(e, "ResourceDict", "FontSet", 1, "Name"); s != "Beleren)Bold" {
		t.Errorf("escaped name %q", s)
	}
	if v := engineGet(e, "EngineDict", "StyleRun", "RunArray", 0, "StyleSheet", "StyleSheetData", "FontSize"); v != 12.5 {
		t.Errorf("float %#v", v)
	}
	if v := engineGet(e, "EngineDict", "StyleRun", "RunLengthArray", 1); v != 6 {
		t.Errorf("int %#v", v)
	}
	if v := engineGet(e, "EngineDict", "StyleRun", "RunArray", 0, "StyleSheet", "StyleSheetData", "FillColor", "Values", 2); v != 0.0 {
		t.Errorf("leading dot float %#v", v)
	}
	if v := engineGet(e, "ResourceDict", "Kind"); v != Name("Name") {
		t.Errorf("name value %#v", v)
	}
	if v := engineGet(e, "ResourceDict", "Plain"); v != "abc" {
		t.Errorf("plain string %#v", v)
	}
	if v := engineGet(e, "EngineDict", "StyleRun", "RunArray", 1, "StyleSheet", "StyleSheetData", "FauxBold"); v != true {
		t.Errorf("bool %#v", v)
	}
	for _, bad := range []string{"<< /A [ 1 2 >>", "<< /A (abc >>", "[ 1 ]", "<< A 1 >>", "<<"} {
		if _, err := ParseEngineData([]byte(bad)); err == nil {
			t.Errorf("%q should fail", bad)
		}
	}
}
