package psd

import "testing"

func TestMacRoman(t *testing.T) {
	if got := macRoman([]byte{'C', 0x8E, 'f', 0xA5}); got != "Céf•" {
		t.Errorf("got %q", got)
	}
	if len(macRomanHigh) != 128 {
		t.Errorf("table has %d entries", len(macRomanHigh))
	}
}

func TestCursorShortReads(t *testing.T) {
	c := newCursor([]byte{0, 0, 0, 9, 'a'})
	if s := c.unicode(); s != "" || c.err == nil {
		t.Errorf("a unicode string longer than its data should fail, got %q %v", s, c.err)
	}
	c = newCursor([]byte{1, 2})
	c.u32()
	if c.err == nil || c.u8() != 0 {
		t.Error("reads after an error return zero")
	}
}
