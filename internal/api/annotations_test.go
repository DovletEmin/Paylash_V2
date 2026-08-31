package api

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// A layer the client would actually send: one pen stroke and one arrow, in
// normalised image coordinates.
const realisticLayer = `[
	{"t":"pen","c":"#ff3b30","w":0.004,"o":1,"p":[[0.1,0.1],[0.11,0.12],[0.13,0.15]]},
	{"t":"arrow","c":"#0a84ff","w":0.006,"o":1,"p":[[0.2,0.2],[0.6,0.7]]}
]`

func TestValidateAnnotationAcceptsRealDrawing(t *testing.T) {
	if err := validateAnnotationShapes(json.RawMessage(realisticLayer)); err != nil {
		t.Fatalf("a normal layer was rejected: %v", err)
	}
}

// An absent or empty layer is the "nothing drawn" case, not an error — the
// client sends it after erasing everything and the handler turns it into a
// delete.
func TestValidateAnnotationAcceptsEmpty(t *testing.T) {
	for _, raw := range []string{"", "[]"} {
		if err := validateAnnotationShapes(json.RawMessage(raw)); err != nil {
			t.Errorf("empty layer %q rejected: %v", raw, err)
		}
		if !annotationIsEmpty(json.RawMessage(raw)) {
			t.Errorf("annotationIsEmpty(%q) = false, want true", raw)
		}
	}
	if annotationIsEmpty(json.RawMessage(realisticLayer)) {
		t.Error("a layer with shapes was reported empty")
	}
}

// The client owns the tool vocabulary, so a shape type the server has never
// heard of must still be stored — that is the whole point of validating the
// envelope rather than the grammar.
func TestValidateAnnotationAcceptsUnknownToolsAndFields(t *testing.T) {
	future := `[{"t":"speech-bubble","c":"#112233","tail":{"x":0.4,"y":0.9},"curve":true,"p":[[0.1,0.1]]}]`
	if err := validateAnnotationShapes(json.RawMessage(future)); err != nil {
		t.Fatalf("a tool the server doesn't know was rejected: %v", err)
	}
}

func TestValidateAnnotationRejectsMalformed(t *testing.T) {
	cases := map[string]string{
		"not an array":        `{"t":"pen"}`,
		"not valid json":      `[{"t":`,
		"shape is not object": `["pen"]`,
		"shape has no type":   `[{"c":"#ffffff","p":[[0,0]]}]`,
		"type is not string":  `[{"t":7}]`,
		"type is empty":       `[{"t":""}]`,
		"type is absurd":      `[{"t":"` + strings.Repeat("x", 17) + `"}]`,
	}
	for name, raw := range cases {
		if err := validateAnnotationShapes(json.RawMessage(raw)); err == nil {
			t.Errorf("%s: accepted, want rejected", name)
		}
	}
}

// The limits that stop a hostile client from filling the database or
// wedging a canvas. Each is checked just past its boundary.
func TestValidateAnnotationEnforcesBounds(t *testing.T) {
	t.Run("too many shapes", func(t *testing.T) {
		shapes := make([]string, maxAnnotationShapes+1)
		for i := range shapes {
			shapes[i] = `{"t":"pen","p":[[0.1,0.1]]}`
		}
		raw := "[" + strings.Join(shapes, ",") + "]"
		if err := validateAnnotationShapes(json.RawMessage(raw)); err == nil {
			t.Error("accepted more than maxAnnotationShapes")
		}
	})

	t.Run("payload too large", func(t *testing.T) {
		raw := `[{"t":"pen","note":"` + strings.Repeat("a", maxAnnotationBytes) + `"}]`
		if err := validateAnnotationShapes(json.RawMessage(raw)); err == nil {
			t.Error("accepted a payload past maxAnnotationBytes")
		}
	})

	// The one a per-shape cap alone would miss: few shapes, enormous
	// polylines. It has to be caught by the node budget spanning the layer.
	t.Run("one shape with a ruinous polyline", func(t *testing.T) {
		pts := make([]string, maxAnnotationNodes)
		for i := range pts {
			pts[i] = "[0.5,0.5]"
		}
		raw := `[{"t":"pen","p":[` + strings.Join(pts, ",") + `]}]`
		if err := validateAnnotationShapes(json.RawMessage(raw)); err == nil {
			t.Error("accepted a single shape exceeding the node budget")
		}
	})

	t.Run("string too long", func(t *testing.T) {
		raw := `[{"t":"text","txt":"` + strings.Repeat("x", maxAnnotationString+1) + `"}]`
		if err := validateAnnotationShapes(json.RawMessage(raw)); err == nil {
			t.Error("accepted an over-long string")
		}
	})

	t.Run("nested too deep", func(t *testing.T) {
		raw := `[{"t":"pen","p":` + strings.Repeat("[", maxAnnotationDepth+1) + strings.Repeat("]", maxAnnotationDepth+1) + `}]`
		if err := validateAnnotationShapes(json.RawMessage(raw)); err == nil {
			t.Error("accepted a layer nested past maxAnnotationDepth")
		}
	})

	t.Run("too many keys", func(t *testing.T) {
		var sb strings.Builder
		sb.WriteString(`[{"t":"pen"`)
		for i := 0; i <= maxAnnotationKeys; i++ {
			fmt.Fprintf(&sb, `,"k%d":1`, i)
		}
		sb.WriteString("}]")
		if err := validateAnnotationShapes(json.RawMessage(sb.String())); err == nil {
			t.Error("accepted a shape with too many keys")
		}
	})

	t.Run("key name too long", func(t *testing.T) {
		raw := `[{"t":"pen","` + strings.Repeat("k", maxAnnotationKeyLen+1) + `":1}]`
		if err := validateAnnotationShapes(json.RawMessage(raw)); err == nil {
			t.Error("accepted an over-long key")
		}
	})
}

// Numbers are the subtle one. A literal like 1e400 decodes to +Inf, which
// then compares false against every bound and would sail through a naive
// check — hence json.Number and the explicit IsInf test.
func TestValidateAnnotationRejectsPathologicalNumbers(t *testing.T) {
	cases := map[string]string{
		"overflows to +Inf":  `[{"t":"pen","p":[[1e400,0.5]]}]`,
		"overflows to -Inf":  `[{"t":"pen","p":[[-1e400,0.5]]}]`,
		"far outside canvas": fmt.Sprintf(`[{"t":"pen","p":[[%d,0.5]]}]`, maxAnnotationNumber+1),
		"absurd width":       fmt.Sprintf(`[{"t":"pen","w":%d}]`, maxAnnotationNumber*10),
	}
	for name, raw := range cases {
		if err := validateAnnotationShapes(json.RawMessage(raw)); err == nil {
			t.Errorf("%s: accepted, want rejected", name)
		}
	}

	// A stroke that runs a little off the edge of the image is normal —
	// people draw past the border — and must not be mistaken for an attack.
	if err := validateAnnotationShapes(json.RawMessage(`[{"t":"pen","p":[[-0.2,1.3],[1.1,-0.05]]}]`)); err != nil {
		t.Errorf("a stroke running slightly off-image was rejected: %v", err)
	}
}
