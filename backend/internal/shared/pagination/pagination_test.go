package pagination

import (
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// The compound cursor (createdAt + _id) is what the #110 E1 fix relies
// on for stable traces/users pagination. Round-trip must preserve both.
func TestCursor_RoundTrip(t *testing.T) {
	when := time.Date(2026, 5, 20, 8, 30, 15, 0, time.UTC)
	enc := EncodeCursor(when, "llm_abc123")
	got, err := DecodeCursor(enc)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.CreatedAt.Equal(when) {
		t.Errorf("createdAt: got %v, want %v", got.CreatedAt, when)
	}
	if got.ID != "llm_abc123" {
		t.Errorf("id: got %q, want llm_abc123", got.ID)
	}
}

func TestDecodeCursor_Garbage(t *testing.T) {
	if _, err := DecodeCursor("not-valid-base64-$$$"); err == nil {
		t.Error("expected error decoding garbage cursor")
	}
}

// BuildFilter must produce the compound predicate matching the
// createdAt-desc,_id-desc sort: strictly older createdAt, OR same
// createdAt with a strictly smaller _id. (The old _id-only filter is
// what skipped/duplicated rows when _id wasn't time-correlated.)
func TestBuildFilter_CompoundPredicate(t *testing.T) {
	when := time.Date(2026, 5, 20, 8, 30, 15, 0, time.UTC)
	c := &Cursor{CreatedAt: when, ID: "x9"}
	f := BuildFilter(bson.M{"userId": "u1"}, c)

	if f["userId"] != "u1" {
		t.Error("existing filter keys must be preserved")
	}
	or, ok := f["$or"].(bson.A)
	if !ok || len(or) != 2 {
		t.Fatalf("expected $or with 2 clauses, got %#v", f["$or"])
	}
	first, _ := or[0].(bson.M)
	if first["createdAt"] == nil {
		t.Error("first $or clause should bound createdAt")
	}
	second, _ := or[1].(bson.M)
	if second["createdAt"] != when || second["_id"] == nil {
		t.Errorf("second $or clause should pin createdAt and bound _id, got %#v", second)
	}
}

func TestBuildFilter_NilCursorUnchanged(t *testing.T) {
	f := BuildFilter(bson.M{"userId": "u1"}, nil)
	if _, has := f["$or"]; has {
		t.Error("nil cursor must not add an $or clause")
	}
}
