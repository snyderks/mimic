// Package psd reads Adobe Photoshop documents
//
// Reading is lazy. Open and Decode read the header, the image resources, every
// layer record with its additional layer information, and where each channel's
// pixels lie in the file. Pixels are decoded only when Layer.Image,
// Layer.MaskImage or File.Composite asks for them, so a document of a gigabyte
// opens in a moment and holds little memory
//
// Layer trees follow the layers panel: a group's Children and the root's run
// from the top of the panel to the bottom. File.Layers keeps the file's own
// order, which is the reverse, bottom to top
package psd

import (
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"io"
	"os"
)

// File is a parsed Photoshop document
type File struct {
	Width, Height int
	// Depth is the bits per channel. Pixel decoding needs 8
	Depth int
	// ColorMode is 3 for RGB. Pixel decoding needs RGB
	ColorMode int
	// Channels is the channel count of the merged image
	Channels int
	// Large is true for a PSB document
	Large bool
	// Resolution is in pixels per inch, 0 when the document does not say
	Resolution float64
	// GlobalAngle and GlobalAltitude are the document's global light in degrees,
	// 30 and 30 when the document does not say
	GlobalAngle, GlobalAltitude float64
	// Root is a synthetic group holding the top level layers, top to bottom
	Root *Layer
	// Layers lists every layer and group in file order, bottom to top, without
	// the hidden section divider records that bound groups
	Layers []*Layer
	// Resources holds each image resource's data by its id
	Resources map[int][]byte
	// Info holds the document's global additional layer information blocks by
	// key. A block larger than maxInlineBlock is left in the file and listed
	// only in Blocks
	Info map[string][]byte
	// Blocks lists every global block with where its data lies, in file order
	Blocks []Block
	// MergedAlpha reports whether the merged image's first extra channel is its
	// transparency
	MergedAlpha bool

	r            io.ReaderAt
	size         int64
	closer       io.Closer
	mergedOffset int64
}

// Block is an additional layer information block and where its data lies
type Block struct {
	Key    string
	Offset int64
	Length int64
}

// maxInlineBlock is the largest global block kept in memory when the file opens
const maxInlineBlock = 16 << 20

// Open opens and parses the document at path. The file stays open for pixel
// decoding until Close
func Open(path string) (*File, error) {
	fh, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	st, err := fh.Stat()
	if err != nil {
		fh.Close()
		return nil, err
	}
	f, err := Decode(fh, st.Size())
	if err != nil {
		fh.Close()
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	f.closer = fh
	return f, nil
}

// Close closes the file Open opened. It does nothing for a File from Decode
func (f *File) Close() error {
	if f.closer == nil {
		return nil
	}
	err := f.closer.Close()
	f.closer = nil
	return err
}

// ReadBlock returns a global block's data, reading it from the file when Info
// does not hold it
func (f *File) ReadBlock(b Block) ([]byte, error) {
	if d, ok := f.Info[b.Key]; ok && int64(len(d)) == b.Length {
		return d, nil
	}
	buf := make([]byte, b.Length)
	if _, err := f.r.ReadAt(buf, b.Offset); err != nil {
		return nil, fmt.Errorf("psd: reading block %q: %w", b.Key, err)
	}
	return buf, nil
}

// Decode parses the document in r, which is size bytes long. Pixel decoding
// reads from r later, so it must stay readable while the File is used
func Decode(r io.ReaderAt, size int64) (*File, error) {
	f := &File{
		r:              r,
		size:           size,
		Resources:      map[int][]byte{},
		Info:           map[string][]byte{},
		GlobalAngle:    30,
		GlobalAltitude: 30,
		Root:           &Layer{Kind: KindGroup, Visible: true, Opacity: 255, FillOpacity: 255, GroupOpen: true},
	}
	s := newStream(r, 0, size)
	if sig := s.key4(); sig != "8BPS" {
		if s.err != nil {
			return nil, s.err
		}
		return nil, fmt.Errorf("psd: not a Photoshop document, signature %q", sig)
	}
	switch v := s.u16(); v {
	case 1:
	case 2:
		f.Large = true
	default:
		return nil, fmt.Errorf("psd: unsupported version %d", v)
	}
	s.read(6)
	f.Channels = int(s.u16())
	f.Height = int(s.u32())
	f.Width = int(s.u32())
	f.Depth = int(s.u16())
	f.ColorMode = int(s.u16())
	if s.err != nil {
		return nil, s.err
	}

	cm := int64(s.u32())
	s.seek(s.pos + cm)

	resLen := int64(s.u32())
	res := s.copyN(resLen)
	if s.err != nil {
		return nil, fmt.Errorf("psd: image resources: %w", s.err)
	}
	f.parseResources(res)

	lmLen := s.length(f.Large)
	lmStart := s.pos
	lmEnd := lmStart + lmLen
	if s.err != nil {
		return nil, s.err
	}
	if lmEnd > size {
		return nil, fmt.Errorf("psd: layer and mask section ends at %d past the file's end at %d", lmEnd, size)
	}
	f.mergedOffset = lmEnd

	var records []*Layer
	negative := false
	if lmLen > 0 {
		ls := newStream(r, lmStart, lmEnd)
		liLen := ls.length(f.Large)
		liStart := ls.pos
		if ls.err != nil {
			return nil, ls.err
		}
		if liLen > 0 {
			var err error
			records, negative, err = f.parseLayerInfo(newStream(r, liStart, liStart+liLen))
			if err != nil {
				return nil, err
			}
		}
		ls.seek(liStart + liLen)
		if ls.pos+4 <= lmEnd {
			peek := ls.read(4)
			ls.seek(ls.pos - 4)
			if peek != nil && string(peek) != "8BIM" && string(peek) != "8B64" {
				gl := int64(ls.u32())
				ls.seek(ls.pos + gl)
			}
		}
		if ls.err != nil {
			return nil, fmt.Errorf("psd: global layer mask: %w", ls.err)
		}
		if err := f.parseGlobalBlocks(ls, lmEnd); err != nil {
			return nil, err
		}
		if len(records) == 0 {
			for _, key := range []string{"Lr16", "Lr32"} {
				for _, b := range f.Blocks {
					if b.Key == key && b.Length > 0 {
						var err error
						records, negative, err = f.parseLayerInfo(newStream(r, b.Offset, b.Offset+b.Length))
						if err != nil {
							return nil, err
						}
					}
				}
			}
		}
	}
	_, mtrn := f.Info["Mtrn"]
	_, mt16 := f.Info["Mt16"]
	_, mt32 := f.Info["Mt32"]
	f.MergedAlpha = f.Channels > 3 && (negative || mtrn || mt16 || mt32)
	if err := f.buildTree(records); err != nil {
		return nil, err
	}
	return f, nil
}

// parseResources reads the image resource blocks. A damaged block ends the walk
// and keeps what came before it
func (f *File) parseResources(b []byte) {
	c := newCursor(b)
	for c.remaining() >= 12 && c.err == nil {
		if sig := c.key4(); sig != "8BIM" && sig != "MeSa" && sig != "AgHg" && sig != "PHUT" && sig != "DCSR" {
			return
		}
		id := int(c.u16())
		c.pascal(2)
		n := int(c.u32())
		data := c.take(n)
		if n%2 == 1 {
			c.skip(1)
		}
		if data == nil {
			return
		}
		f.Resources[id] = data
	}
	if d := f.Resources[1005]; len(d) >= 4 {
		f.Resolution = float64(binary.BigEndian.Uint32(d)) / 65536
	}
	if d := f.Resources[1037]; len(d) >= 4 {
		f.GlobalAngle = float64(int32(binary.BigEndian.Uint32(d)))
	}
	if d := f.Resources[1049]; len(d) >= 4 {
		f.GlobalAltitude = float64(int32(binary.BigEndian.Uint32(d)))
	}
}

// bigKeys lists the keys whose block length is 8 bytes in a PSB document
var bigKeys = map[string]bool{
	"LMsk": true, "Lr16": true, "Lr32": true, "Layr": true, "Mt16": true, "Mt32": true,
	"Mtrn": true, "Alph": true, "FMsk": true, "lnk2": true, "FEid": true, "FXid": true,
	"PxSD": true, "lnkE": true, "pths": true, "cinf": true,
}

// parseGlobalBlocks reads the additional layer information blocks after the
// global layer mask. Photoshop pads each to a multiple of 4 bytes outside its
// declared length
func (f *File) parseGlobalBlocks(s *stream, end int64) error {
	for s.pos+12 <= end {
		sig := s.key4()
		if sig != "8BIM" && sig != "8B64" {
			break
		}
		key := s.key4()
		n := s.length(f.Large && bigKeys[key])
		if s.err != nil {
			return fmt.Errorf("psd: global block %q: %w", key, s.err)
		}
		off := s.pos
		if off+n > end {
			return fmt.Errorf("psd: global block %q of %d bytes passes the end of its section", key, n)
		}
		f.Blocks = append(f.Blocks, Block{Key: key, Offset: off, Length: n})
		if n <= maxInlineBlock {
			f.Info[key] = s.copyN(n)
		} else {
			s.seek(off + n)
		}
		next := off + n
		if r := n % 4; r != 0 && !f.signatureAt(next) && f.signatureAt(next+4-r) {
			next += 4 - r
		}
		s.seek(next)
		if s.err != nil {
			return fmt.Errorf("psd: global block %q: %w", key, s.err)
		}
	}
	return nil
}

func (f *File) signatureAt(off int64) bool {
	var b [4]byte
	if off < 0 || off+4 > f.size {
		return false
	}
	if _, err := f.r.ReadAt(b[:], off); err != nil {
		return false
	}
	return string(b[:]) == "8BIM" || string(b[:]) == "8B64"
}

// Find returns the layer reached by following names from the root, or nil. A
// name matches the first child with it, from the top of the panel
func (f *File) Find(path ...string) *Layer {
	l := f.Root
	for _, name := range path {
		var next *Layer
		for _, c := range l.Children {
			if c.Name == name {
				next = c
				break
			}
		}
		if next == nil {
			return nil
		}
		l = next
	}
	if l == f.Root {
		return nil
	}
	return l
}

// Walk calls fn for every layer below the root, depth first in panel order, and
// skips a group's children when fn returns false
func (f *File) Walk(fn func(*Layer) bool) {
	var walk func(*Layer)
	walk = func(l *Layer) {
		for _, c := range l.Children {
			if fn(c) {
				walk(c)
			}
		}
	}
	walk(f.Root)
}

// errPixelFormat reports a document whose pixels this package does not decode
var errPixelFormat = errors.New("psd: pixel decoding needs an 8 bit RGB document")

func (f *File) checkPixels() error {
	if f.Depth != 8 || f.ColorMode != 3 {
		return fmt.Errorf("%w, this one is %d bit in color mode %d", errPixelFormat, f.Depth, f.ColorMode)
	}
	return nil
}

// Composite decodes the merged image Photoshop stored with the document, the
// flattened picture as it last saved it. Alpha comes from the first extra
// channel when MergedAlpha is set and is opaque otherwise. Photoshop stores a
// transparent merged image matted against white, and Composite removes that
// matte so the colors are straight
func (f *File) Composite() (*image.NRGBA, error) {
	if err := f.checkPixels(); err != nil {
		return nil, err
	}
	w, h := f.Width, f.Height
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	if w == 0 || h == 0 {
		return img, nil
	}
	n := 3
	if f.MergedAlpha {
		n = 4
	}
	if f.Channels < n {
		n = f.Channels
	}
	var hdr [2]byte
	if _, err := f.r.ReadAt(hdr[:], f.mergedOffset); err != nil {
		return nil, fmt.Errorf("psd: merged image: %w", err)
	}
	comp := int(binary.BigEndian.Uint16(hdr[:]))
	start := f.mergedOffset + 2
	plane := int64(w) * int64(h)
	chans := make([]channel, n)
	switch comp {
	case 0:
		for i := range chans {
			chans[i] = channel{ID: i, offset: start + int64(i)*plane, length: plane, compression: 0, bare: true}
		}
	case 1:
		cw := int64(2)
		if f.Large {
			cw = 4
		}
		rows := int64(f.Channels) * int64(h)
		counts := make([]byte, rows*cw)
		if _, err := f.r.ReadAt(counts, start); err != nil {
			return nil, fmt.Errorf("psd: merged image row counts: %w", err)
		}
		off := start + int64(len(counts))
		for i := 0; i < f.Channels && i < n; i++ {
			var total int64
			for y := range int64(h) {
				k := (int64(i)*int64(h) + y) * cw
				if cw == 2 {
					total += int64(binary.BigEndian.Uint16(counts[k:]))
				} else {
					total += int64(binary.BigEndian.Uint32(counts[k:]))
				}
			}
			chans[i] = channel{ID: i, offset: off, length: total, compression: 1, bare: true, counts: counts[int64(i)*int64(h)*cw : (int64(i)+1)*int64(h)*cw]}
			off += total
		}
	default:
		return nil, fmt.Errorf("psd: merged image compression %d is not supported", comp)
	}
	planes := make([][]byte, n)
	errs := make([]error, n)
	done := make(chan int, n)
	for i := range chans {
		go func(i int) {
			planes[i], errs[i] = f.decodeChannel(chans[i], w, h)
			done <- i
		}(i)
	}
	for range chans {
		<-done
	}
	for _, err := range errs {
		if err != nil {
			return nil, fmt.Errorf("psd: merged image: %w", err)
		}
	}
	interleave(img, planes[0], planes[1], planes[2], alphaPlane(planes, n))
	if n >= 4 {
		unmatte(img.Pix)
	}
	return img, nil
}

// unmatte undoes compositing against white in single precision with truncation,
// which is the arithmetic psd-tools uses, so both read the same colors
func unmatte(pix []byte) {
	for i := 0; i < len(pix); i += 4 {
		a := pix[i+3]
		if a == 0 || a == 255 {
			continue
		}
		for c := range 3 {
			v := float32(int(pix[i+c])+int(a)-255) * 255
			v /= float32(a)
			switch {
			case v <= 0:
				pix[i+c] = 0
			case v >= 255:
				pix[i+c] = 255
			default:
				pix[i+c] = byte(v)
			}
		}
	}
}

func alphaPlane(planes [][]byte, n int) []byte {
	if n >= 4 {
		return planes[3]
	}
	return nil
}
