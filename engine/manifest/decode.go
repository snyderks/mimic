package manifest

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
)

// Limits on what a manifest may hold. A bundle can come from anyone, so the sizes
// a render allocates from it are bounded where it is read
const (
	// MaxBytes is the largest manifest file that is read
	MaxBytes = 4 << 20
	// MaxDimension is the largest width, height, or position in a canvas
	MaxDimension = 20000
	// MaxParts is the largest number of art sets a template may name. A family
	// template merged from the m15 frames names 270, so the ceiling leaves room
	// for a family a few times that size
	MaxParts     = 1024
	MaxVariants  = 128
	MaxLayouts   = 64
	MaxStack     = 128
	MaxTextBoxes = 64
	MaxFeatures  = 32
	MaxFonts     = 32
	MaxRoles     = 64
	MaxPathBytes = 255
)

var contractRe = regexp.MustCompile(`^(\d+)\.(\d+)$`)

// Decode reads a resolved manifest and checks it. It returns the manifest
// whenever the JSON could be read into one, so a tool can show it beside its
// diagnostics, and a loader refuses it when the diagnostics hold an error. A
// manifest of a different contract major is not returned
func Decode(data []byte) (*Manifest, Diagnostics) { return decode(data, false) }

// DecodeStrict is Decode for a tool that checks a manifest an author wrote, and it
// refuses a field the contract does not have, where a loader skips it
func DecodeStrict(data []byte) (*Manifest, Diagnostics) { return decode(data, true) }

func decode(data []byte, strict bool) (*Manifest, Diagnostics) {
	var c collector
	if len(data) > MaxBytes {
		c.errorf("", "the manifest is %d bytes, over the %d byte limit", len(data), MaxBytes)
		return nil, c.ds
	}
	var m Manifest
	var err error
	if strict {
		err = decodeStrict(data, &m)
	} else {
		err = json.Unmarshal(data, &m)
	}
	if err != nil {
		c.errorf("", "%s", describeJSONError(err))
		return nil, c.ds
	}
	if !c.contract(m.Contract) {
		return nil, c.ds
	}
	c.ds = append(c.ds, Validate(&m)...)
	return &m, c.ds
}

// contract checks the version and reports whether the manifest can be read
func (c *collector) contract(v string) bool {
	match := contractRe.FindStringSubmatch(v)
	if match == nil {
		c.errorf("contract", "%q is not a version like %q", v, Contract)
		return false
	}
	want := contractRe.FindStringSubmatch(Contract)
	if match[1] != want[1] {
		c.errorf("contract", "version %s is not one this engine reads, which reads %s.x", v, want[1])
		return false
	}
	got, _ := strconv.Atoi(match[2])
	have, _ := strconv.Atoi(want[2])
	if got > have {
		c.warnf("contract", "version %s is newer than %s, so some of it may be ignored", v, Contract)
	}
	return true
}

// describeJSONError turns a decoding error into a message with where it was
func describeJSONError(err error) string {
	var syntax *json.SyntaxError
	var typ *json.UnmarshalTypeError
	switch {
	case errors.As(err, &syntax):
		return fmt.Sprintf("the JSON is not valid at byte %d: %v", syntax.Offset, err)
	case errors.As(err, &typ):
		if typ.Field == "" {
			return fmt.Sprintf("the manifest has to be an object, and it is %s", jsonKind(typ.Value))
		}
		return fmt.Sprintf("%s is %s and has to be %s", typ.Field, jsonKind(typ.Value), goKind(typ.Type))
	}
	return err.Error()
}

// jsonKind names a JSON value by the word encoding/json gives its kind
func jsonKind(v string) string {
	switch v {
	case "array":
		return "a list"
	case "object":
		return "an object"
	case "string":
		return "a string"
	case "number":
		return "a number"
	case "bool":
		return "true or false"
	case "null":
		return "null"
	}
	// A number that does not fit its field arrives as "number 1.5"
	if strings.HasPrefix(v, "number") {
		return "a number that does not fit"
	}
	return v
}

// goKind names what a field of the type holds, in JSON's terms
func goKind(t reflect.Type) string {
	switch t.Kind() {
	case reflect.Struct, reflect.Map:
		return "an object"
	case reflect.Slice, reflect.Array:
		return "a list"
	case reflect.String:
		return "a string"
	case reflect.Bool:
		return "true or false"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Float32, reflect.Float64:
		return "a number"
	}
	return t.String()
}
