package psd

import (
	"strings"
	"testing"
)

func TestVectorMaskEdges(t *testing.T) {
	if _, err := parseVectorMask([]byte{0, 0, 0, 2, 0, 0, 0, 0}, 10, 10); err == nil || !strings.Contains(err.Error(), "version") {
		t.Errorf("version 2 should fail: %v", err)
	}
	if _, err := parseVectorMask([]byte{0, 0}, 10, 10); err == nil {
		t.Error("a short block should fail")
	}
	var b bw
	b.u32(3)
	b.u32(4)
	pathRecord(&b, 7, func(r *bw) { r.i32(0) })
	pathRecord(&b, 5, func(r *bw) { knot(r, [3]Point{{1, 1}, {2, 2}, {3, 3}}) })
	pathRecord(&b, 0, func(r *bw) { r.u16(1); r.i16(9) })
	pathRecord(&b, 1, func(r *bw) { knot(r, [3]Point{{4, 4}, {4, 4}, {4, 4}}) })
	b.Write([]byte{0, 1, 2})
	vm, err := parseVectorMask(b.Bytes(), docW, docH)
	if err != nil {
		t.Fatal(err)
	}
	if !vm.Disabled || vm.Inverted || vm.InitialFill != 0 || vm.HasFillRule {
		t.Errorf("flags %+v", vm)
	}
	if len(vm.Subpaths) != 2 {
		t.Fatalf("subpaths %+v", vm.Subpaths)
	}
	if s := vm.Subpaths[0]; s.Closed || s.Operation != -1 || len(s.Knots) != 1 || s.Knots[0].Linked {
		t.Errorf("a knot before any subpath record opens one: %+v", s)
	}
	if s := vm.Subpaths[1]; !s.Closed || s.Operation != -1 || len(s.Knots) != 1 || !s.Knots[0].Linked {
		t.Errorf("an unknown operation reads as -1: %+v", s)
	}
}
