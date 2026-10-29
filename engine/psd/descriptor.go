package psd

import (
	"fmt"
	"strings"
)

// Descriptor is an Action Descriptor, Photoshop's tagged structure for effects,
// fills, text and most settings. Items keep their file
// order. A value is one of
//
//	*Descriptor      for Objc and GlbO
//	[]any            for VlLs
//	float64          for doub
//	UnitFloat        for UntF
//	UnitFloats       for UnFl
//	bool             for bool
//	int32            for long
//	int64            for comp
//	Enum             for enum
//	string           for TEXT
//	RawData          for tdta, alis and Pth
//	Class            for type, GlbC and Clss
//	*ObjectArray     for ObAr
//	Reference        for obj
type Descriptor struct {
	Name  string
	Class string
	Items []Item
}

// Item is one keyed value of a descriptor
type Item struct {
	Key   string
	Value any
}

// UnitFloat is a number with a unit, such as #Ang for degrees, #Pxl for pixels,
// #Prc for percent or #Pnt for points
type UnitFloat struct {
	Unit  string
	Value float64
}

// UnitFloats is a run of numbers sharing a unit
type UnitFloats struct {
	Unit   string
	Values []float64
}

// Enum is an enumerated value and the type it belongs to
type Enum struct {
	Type  string
	Value string
}

// Class names a class
type Class struct {
	Name string
	ID   string
}

// RawData is a value kept as its bytes
type RawData []byte

// ObjectArray is a descriptor whose items each hold one value per object, used
// for the points of a newer vector origination
type ObjectArray struct {
	Count int
	Descriptor
}

// Reference is a reference to an object, kept as its parts
type Reference []RefItem

// RefItem is one part of a reference. Value holds what the form carries: a
// Class, an Enum, a string for a name, an int32 for an identifier, index or
// offset, or a property key as a string
type RefItem struct {
	Form  string
	Class Class
	Value any
}

// maxDescriptorDepth bounds nesting so a damaged file cannot recurse without end
const maxDescriptorDepth = 64

// ParseDescriptor reads a descriptor that starts with its unicode name, as found
// after the version number of a descriptor block
func ParseDescriptor(b []byte) (*Descriptor, error) {
	c := newCursor(b)
	d := readDescriptor(c, 0)
	if c.err != nil {
		return nil, fmt.Errorf("psd: descriptor: %w", c.err)
	}
	return d, nil
}

// descriptorKey reads a key that is either four characters behind a zero length
// or a string of the given length
func descriptorKey(c *cursor) string {
	n := c.u32()
	if n == 0 {
		return c.key4()
	}
	if int64(n) > int64(c.remaining()) {
		c.err = errShort
		return ""
	}
	return string(c.take(int(n)))
}

func readDescriptor(c *cursor, depth int) *Descriptor {
	d := &Descriptor{}
	if depth > maxDescriptorDepth {
		c.err = fmt.Errorf("descriptor nests deeper than %d", maxDescriptorDepth)
		return d
	}
	d.Name = c.unicode()
	d.Class = descriptorKey(c)
	n := c.u32()
	for i := uint32(0); i < n && c.err == nil; i++ {
		key := descriptorKey(c)
		v := readValue(c, c.key4(), depth)
		d.Items = append(d.Items, Item{Key: key, Value: v})
	}
	return d
}

func readValue(c *cursor, ostype string, depth int) any {
	switch ostype {
	case "Objc", "GlbO":
		return readDescriptor(c, depth+1)
	case "VlLs":
		n := c.u32()
		var out []any
		for i := uint32(0); i < n && c.err == nil; i++ {
			out = append(out, readValue(c, c.key4(), depth+1))
		}
		return out
	case "doub":
		return c.f64()
	case "UntF":
		u := c.key4()
		return UnitFloat{Unit: u, Value: c.f64()}
	case "UnFl":
		u := c.key4()
		n := c.u32()
		if int64(n)*8 > int64(c.remaining()) {
			c.err = errShort
			return nil
		}
		vs := make([]float64, n)
		for i := range vs {
			vs[i] = c.f64()
		}
		return UnitFloats{Unit: u, Values: vs}
	case "bool":
		return c.u8() != 0
	case "long":
		return c.i32()
	case "comp":
		return c.i64()
	case "enum":
		t := descriptorKey(c)
		return Enum{Type: t, Value: descriptorKey(c)}
	case "TEXT":
		return c.unicode()
	case "tdta", "alis", "Pth ":
		n := c.u32()
		if int64(n) > int64(c.remaining()) {
			c.err = errShort
			return nil
		}
		return RawData(c.take(int(n)))
	case "type", "GlbC", "Clss":
		name := c.unicode()
		return Class{Name: name, ID: descriptorKey(c)}
	case "ObAr":
		count := c.u32()
		d := readDescriptor(c, depth+1)
		return &ObjectArray{Count: int(count), Descriptor: *d}
	case "obj ":
		n := c.u32()
		var ref Reference
		for i := uint32(0); i < n && c.err == nil; i++ {
			ref = append(ref, readRefItem(c, c.key4()))
		}
		return ref
	}
	if c.err == nil {
		c.err = fmt.Errorf("unknown descriptor value type %q", ostype)
	}
	return nil
}

func readRefItem(c *cursor, form string) RefItem {
	it := RefItem{Form: form}
	switch form {
	case "prop":
		it.Class = Class{Name: c.unicode(), ID: descriptorKey(c)}
		it.Value = descriptorKey(c)
	case "Clss":
		it.Class = Class{Name: c.unicode(), ID: descriptorKey(c)}
	case "Enmr":
		it.Class = Class{Name: c.unicode(), ID: descriptorKey(c)}
		t := descriptorKey(c)
		it.Value = Enum{Type: t, Value: descriptorKey(c)}
	case "rele":
		it.Class = Class{Name: c.unicode(), ID: descriptorKey(c)}
		it.Value = c.i32()
	case "Idnt", "indx":
		it.Value = c.i32()
	case "name":
		it.Class = Class{Name: c.unicode(), ID: descriptorKey(c)}
		it.Value = c.unicode()
	default:
		if c.err == nil {
			c.err = fmt.Errorf("unknown reference form %q", form)
		}
	}
	return it
}

// Get returns the value of the first item with the key
func (d *Descriptor) Get(key string) (any, bool) {
	if d == nil {
		return nil, false
	}
	for _, it := range d.Items {
		if it.Key == key {
			return it.Value, true
		}
	}
	return nil, false
}

// Has reports whether the descriptor holds the key
func (d *Descriptor) Has(key string) bool {
	_, ok := d.Get(key)
	return ok
}

// Float reads a number of any numeric type, ignoring its unit
func (d *Descriptor) Float(key string) (float64, bool) {
	v, ok := d.Get(key)
	if !ok {
		return 0, false
	}
	return toFloat(v)
}

func toFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case UnitFloat:
		return x.Value, true
	case int32:
		return float64(x), true
	case int64:
		return float64(x), true
	case bool:
		if x {
			return 1, true
		}
		return 0, true
	}
	return 0, false
}

// FloatOr reads a number, or returns def when the key is missing
func (d *Descriptor) FloatOr(key string, def float64) float64 {
	if f, ok := d.Float(key); ok {
		return f
	}
	return def
}

// Unit reads a unit float and its unit
func (d *Descriptor) Unit(key string) (UnitFloat, bool) {
	v, ok := d.Get(key)
	if !ok {
		return UnitFloat{}, false
	}
	switch x := v.(type) {
	case UnitFloat:
		return x, true
	default:
		f, ok := toFloat(v)
		return UnitFloat{Unit: "#Nne", Value: f}, ok
	}
}

// Int reads an integer, rounding toward zero a value stored as a float
func (d *Descriptor) Int(key string) (int, bool) {
	f, ok := d.Float(key)
	return int(f), ok
}

// Bool reads a boolean
func (d *Descriptor) Bool(key string) (bool, bool) {
	v, ok := d.Get(key)
	if !ok {
		return false, false
	}
	switch x := v.(type) {
	case bool:
		return x, true
	case int32:
		return x != 0, true
	}
	return false, false
}

// BoolOr reads a boolean, or returns def when the key is missing
func (d *Descriptor) BoolOr(key string, def bool) bool {
	if b, ok := d.Bool(key); ok {
		return b
	}
	return def
}

// String reads a text value
func (d *Descriptor) String(key string) (string, bool) {
	v, ok := d.Get(key)
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

// Enum reads the value of an enumerated item
func (d *Descriptor) Enum(key string) (string, bool) {
	v, ok := d.Get(key)
	if !ok {
		return "", false
	}
	e, ok := v.(Enum)
	return e.Value, ok
}

// Desc reads a nested descriptor
func (d *Descriptor) Desc(key string) *Descriptor {
	v, _ := d.Get(key)
	switch x := v.(type) {
	case *Descriptor:
		return x
	case *ObjectArray:
		return &x.Descriptor
	}
	return nil
}

// List reads a list
func (d *Descriptor) List(key string) []any {
	v, _ := d.Get(key)
	l, _ := v.([]any)
	return l
}

// Raw reads raw data
func (d *Descriptor) Raw(key string) []byte {
	v, _ := d.Get(key)
	r, _ := v.(RawData)
	return r
}

// Path follows nested descriptors by key
func (d *Descriptor) Path(keys ...string) *Descriptor {
	for _, k := range keys {
		if d == nil {
			return nil
		}
		d = d.Desc(k)
	}
	return d
}

// Dump renders the descriptor as indented text for inspection
func (d *Descriptor) Dump() string {
	var sb strings.Builder
	dumpValue(&sb, d, 0)
	return sb.String()
}

func dumpValue(sb *strings.Builder, v any, indent int) {
	pad := strings.Repeat("  ", indent)
	switch x := v.(type) {
	case *Descriptor:
		fmt.Fprintf(sb, "%s{\n", x.Class)
		for _, it := range x.Items {
			fmt.Fprintf(sb, "%s  %q: ", pad, it.Key)
			dumpValue(sb, it.Value, indent+1)
			sb.WriteString("\n")
		}
		sb.WriteString(pad + "}")
	case *ObjectArray:
		fmt.Fprintf(sb, "ObAr[%d] ", x.Count)
		dumpValue(sb, &x.Descriptor, indent)
	case []any:
		sb.WriteString("[\n")
		for _, e := range x {
			sb.WriteString(pad + "  ")
			dumpValue(sb, e, indent+1)
			sb.WriteString("\n")
		}
		sb.WriteString(pad + "]")
	case RawData:
		fmt.Fprintf(sb, "raw(%d bytes)", len(x))
	case string:
		fmt.Fprintf(sb, "%q", x)
	default:
		fmt.Fprintf(sb, "%v", x)
	}
}
