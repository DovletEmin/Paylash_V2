package api

import (
	"bytes"
	"encoding/json"
	"math"
	"net/http"
	"strconv"

	"paylash/internal/authutil"
	"paylash/internal/models"
)

/*
Markup layers drawn over a previewed image — the freehand counterpart to the
pinned notes in comments.go.

The server stores each layer's shapes as opaque JSON rather than modelling
the drawing grammar in Go. That is a deliberate choice: the client owns the
vocabulary of tools, so adding an arrow style or a new shape is a frontend
change alone, with no matching server release and no migration. What the
server does own is the *envelope* — how big a layer may be, how deeply it may
nest, how long its strings may run and how large its numbers may get. Those
are the properties that decide whether a hostile or buggy client can exhaust
storage or wedge a renderer, and they are checked generically
(validateAnnotationShapes) so they keep holding for tools that don't exist
yet.
*/

const (
	// A layer is one person's markup on one image. 256KB of JSON is already
	// thousands of strokes; well past this the drawing is no longer
	// something a human made by hand on a photo.
	maxAnnotationBytes = 256 << 10

	maxAnnotationShapes = 1500
	// Bounds the total work any renderer has to do for one layer, which a
	// per-shape limit alone would not: 1500 shapes each holding a
	// 100k-point polyline is small in shape count and ruinous in practice.
	maxAnnotationNodes  = 60000
	maxAnnotationDepth  = 6
	maxAnnotationString = 2000
	maxAnnotationKeys   = 24
	maxAnnotationKeyLen = 32
	// Coordinates are normalised to the image (0..1), so this is enormously
	// generous — it exists only to keep a nonsense value from reaching a
	// canvas draw call, not to police legitimate drawing.
	maxAnnotationNumber = 10000
)

// ListFileAnnotations returns every author's layer for a file. Reading is a
// view-level action, exactly like reading comments: anyone who can open the
// image can see the markup on it.
func (h *Handler) ListFileAnnotations(w http.ResponseWriter, r *http.Request) {
	user := authutil.GetUser(r)
	fileID, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "nädogry ID")
		return
	}

	canAccess, err := h.db.CanAccessFile(fileID, user.ID, user.Role == "admin", "view")
	if err != nil || !canAccess {
		writeError(w, http.StatusForbidden, "rugsat ýok")
		return
	}

	list, err := h.db.ListAnnotations(fileID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "bellikleri alyp bolmady")
		return
	}
	if list == nil {
		list = []models.FileAnnotation{}
	}
	writeJSON(w, http.StatusOK, list)
}

// SaveFileAnnotation replaces the caller's own layer on a file.
//
// Like commenting, this is gated on VIEW access rather than edit: marking up
// a render is how a reviewer responds to it, and a reviewer is precisely the
// person who was given read-only access. It is also why this can never touch
// anyone else's work — the row is keyed by (file, caller), so the only thing
// a user can overwrite here is their own previous drawing.
func (h *Handler) SaveFileAnnotation(w http.ResponseWriter, r *http.Request) {
	user := authutil.GetUser(r)
	fileID, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "nädogry ID")
		return
	}

	canAccess, err := h.db.CanAccessFile(fileID, user.ID, user.Role == "admin", "view")
	if err != nil || !canAccess {
		writeError(w, http.StatusForbidden, "rugsat ýok")
		return
	}

	// Deliberately NOT rate limited, unlike commenting.
	//
	// A comment is a human action — one deliberate act per post — so the
	// comment limiter's 30 per 10 minutes (3/min) sits far above anything
	// real. This is an autosave, which is a machine cadence: the client
	// writes the layer about a second after each change. Measured against a
	// normal review pass — thirty short strokes with a pause between each —
	// that comes out at 57 saves/min, nineteen times the comment ceiling.
	// Borrowing that limiter here meant roughly half a minute of drawing,
	// then ten solid minutes of 429s with the work stranded in the browser.
	//
	// Nothing unbounded is left open by dropping it. The row is an upsert
	// keyed by (file, author), so repeated writes replace one row instead of
	// accumulating; its size is capped by maxAnnotationBytes and its parse
	// cost by validateAnnotationShapes. What remains is an authenticated LAN
	// user choosing to spend their own bandwidth, which is true of nearly
	// every endpoint here and is not what a per-action limiter is for.
	var req struct {
		Shapes json.RawMessage `json:"shapes"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "nädogry maglumat")
		return
	}
	if err := validateAnnotationShapes(req.Shapes); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// An empty layer is a deletion, not a row full of nothing — otherwise
	// erasing everything would leave the author listed as having markup on
	// the image forever.
	if annotationIsEmpty(req.Shapes) {
		if existing, err := h.db.GetAnnotationFor(fileID, user.ID); err == nil && existing != nil {
			if err := h.db.DeleteAnnotation(existing.ID); err != nil {
				writeError(w, http.StatusInternalServerError, "bellikleri pozup bolmady")
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	a, err := h.db.SaveAnnotation(fileID, user.ID, req.Shapes)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "bellikleri ýazdyryp bolmady")
		return
	}
	a.UserName = displayNameOrUsername(user)
	a.UserAvatar = user.AvatarURL
	writeJSON(w, http.StatusOK, a)
}

// DeleteFileAnnotation removes one layer. The circle allowed to clear
// someone else's markup is the same one that can delete their comment: the
// author, the file's owner, or an admin.
func (h *Handler) DeleteFileAnnotation(w http.ResponseWriter, r *http.Request) {
	user := authutil.GetUser(r)
	annID, err := strconv.Atoi(r.PathValue("annotationId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "nädogry ID")
		return
	}

	ann, err := h.db.GetAnnotation(annID)
	if err != nil || ann == nil {
		writeError(w, http.StatusNotFound, "bellik tapylmady")
		return
	}

	if ann.UserID != user.ID && user.Role != "admin" {
		f, err := h.db.GetFile(ann.FileID)
		if err != nil || f == nil || f.OwnerID != user.ID {
			writeError(w, http.StatusForbidden, "rugsat ýok")
			return
		}
	}

	if err := h.db.DeleteAnnotation(annID); err != nil {
		writeError(w, http.StatusInternalServerError, "bellikleri pozup bolmady")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// annotationIsEmpty reports whether the payload carries no shapes at all —
// a missing field, JSON null, or the empty array.
func annotationIsEmpty(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return true
	}
	var shapes []json.RawMessage
	if err := json.Unmarshal(raw, &shapes); err != nil {
		return true
	}
	return len(shapes) == 0
}

// annotationError is a validation failure phrased for the client. Every
// message is deliberately vague about which limit was hit: the limits are
// far above anything real use produces, so a caller seeing one is either
// buggy or probing, and neither benefits from the exact number.
type annotationError struct{ msg string }

func (e *annotationError) Error() string { return e.msg }

func annErr(msg string) error { return &annotationError{msg: msg} }

// validateAnnotationShapes checks the envelope of a markup layer without
// interpreting the drawing itself. See the package comment at the top of
// this file for why the grammar stays the client's business and only the
// bounds are enforced here.
func validateAnnotationShapes(raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil // treated as "no markup" by annotationIsEmpty
	}
	if len(raw) > maxAnnotationBytes {
		return annErr("bellikler gaty uly")
	}

	var shapes []json.RawMessage
	if err := json.Unmarshal(raw, &shapes); err != nil {
		return annErr("nädogry bellik maglumaty")
	}
	if len(shapes) > maxAnnotationShapes {
		return annErr("bellikler gaty köp")
	}

	nodes := 0
	for _, s := range shapes {
		var v any
		// UseNumber keeps big literals out of float64 until walkAnnotation
		// can range-check them: plain decoding turns 1e400 into +Inf, which
		// then compares false against every bound and slips through.
		dec := json.NewDecoder(bytes.NewReader(s))
		dec.UseNumber()
		if err := dec.Decode(&v); err != nil {
			return annErr("nädogry bellik maglumaty")
		}
		obj, ok := v.(map[string]any)
		if !ok {
			return annErr("nädogry bellik maglumaty")
		}
		// The one grammatical rule worth keeping server-side: a shape has to
		// say what it is, or nothing downstream can decide how to draw it.
		t, ok := obj["t"].(string)
		if !ok || t == "" || len(t) > 16 {
			return annErr("nädogry bellik görnüşi")
		}
		if err := walkAnnotation(v, 1, &nodes); err != nil {
			return err
		}
	}
	return nil
}

// walkAnnotation enforces the depth, size and range limits over an already
// decoded shape, counting every value it visits into nodes so the caller can
// bound the layer as a whole rather than each shape in isolation.
func walkAnnotation(v any, depth int, nodes *int) error {
	if depth > maxAnnotationDepth {
		return annErr("bellik maglumaty gaty çylşyrymly")
	}
	*nodes++
	if *nodes > maxAnnotationNodes {
		return annErr("bellikler gaty uly")
	}

	switch x := v.(type) {
	case map[string]any:
		if len(x) > maxAnnotationKeys {
			return annErr("nädogry bellik maglumaty")
		}
		for k, sub := range x {
			if len(k) > maxAnnotationKeyLen {
				return annErr("nädogry bellik maglumaty")
			}
			if err := walkAnnotation(sub, depth+1, nodes); err != nil {
				return err
			}
		}
	case []any:
		for _, sub := range x {
			if err := walkAnnotation(sub, depth+1, nodes); err != nil {
				return err
			}
		}
	case string:
		if len(x) > maxAnnotationString {
			return annErr("bellik teksti gaty uzyn")
		}
	case json.Number:
		f, err := x.Float64()
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) || math.Abs(f) > maxAnnotationNumber {
			return annErr("nädogry bellik koordinaty")
		}
	case bool, nil:
		// Nothing to bound.
	default:
		return annErr("nädogry bellik maglumaty")
	}
	return nil
}
