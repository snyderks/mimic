package psd

import (
	"bytes"
	"testing"
)

// channelFile is a File over raw bytes for decodeChannel
func channelFile(b []byte, large bool) *File {
	return &File{r: bytes.NewReader(b), size: int64(len(b)), Large: large}
}

func TestDecodeChannel(t *testing.T) {
	const w, h = 5, 3
	plane := pattern(w*h, 9)
	for comp := range 4 {
		for _, large := range []bool{false, true} {
			data := encodeChannel(plane, w, h, comp, large)
			f := channelFile(data, large)
			got, err := f.decodeChannel(channel{offset: 0, length: int64(len(data))}, w, h)
			if err != nil || !bytes.Equal(got, plane) {
				t.Errorf("compression %d large %v: %v %v", comp, large, got, err)
			}
		}
	}
	f := channelFile([]byte{0, 1}, false)
	if got, err := f.decodeChannel(channel{length: 0}, w, h); err != nil || !bytes.Equal(got, make([]byte, w*h)) {
		t.Errorf("a channel without data decodes to zeros: %v %v", got, err)
	}
}

func TestDecodeChannelErrors(t *testing.T) {
	const w, h = 4, 2
	plane := pattern(w*h, 3)
	cases := map[string][]byte{
		"short raw":        encodeChannel(plane, w, h, 0, false)[:5],
		"short row counts": {0, 1, 0},
		"rows past data":   append([]byte{0, 1, 0, 9, 0, 9}, 3, 1, 2, 3, 4),
		"bad zip":          {0, 2, 1, 2, 3, 4},
		"short zip":        encodeChannel(plane[:4], 4, 1, 2, false),
		"unknown":          {0, 7, 0, 0},
	}
	for name, data := range cases {
		f := channelFile(data, false)
		if _, err := f.decodeChannel(channel{length: int64(len(data))}, w, h); err == nil {
			t.Errorf("%s should fail", name)
		}
	}
	f := channelFile([]byte{0, 0}, false)
	if _, err := f.decodeChannel(channel{offset: 0, length: 10}, w, h); err == nil {
		t.Error("a channel past the end of the file should fail")
	}
}
