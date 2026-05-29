package wardrobe

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// idorRepo wraps the package's fakeWardrobeRepo and tracks whether the
// image-layer methods were reached, so a regression test can assert
// that a non-owner is rejected BEFORE any GridFS access. OwnsItem is
// configurable; the embedded fake provides the rest of the interface.
type idorRepo struct {
	*fakeWardrobeRepo
	owns             bool
	saveImageCalled  bool
	getImageCalled   bool
	updateItemCalled bool
}

func (r *idorRepo) OwnsItem(context.Context, string, string) (bool, error) { return r.owns, nil }
func (r *idorRepo) SaveImage(context.Context, string, []byte, string) error {
	r.saveImageCalled = true
	return nil
}
func (r *idorRepo) GetImage(context.Context, string) ([]byte, string, error) {
	r.getImageCalled = true
	return []byte("img"), "image/jpeg", nil
}
func (r *idorRepo) UpdateItem(context.Context, string, string, map[string]string, string, string) error {
	r.updateItemCalled = true
	return nil
}

func newIDORHandler(repo Repository) *Handler {
	return &Handler{logger: log.New(io.Discard, "", 0), repo: repo}
}

// A different user must not be able to read another user's item image
// (or run external searches on it) via POST /items/{id}/search.
func TestSearch_RejectsNonOwnerBeforeImageRead(t *testing.T) {
	repo := &idorRepo{fakeWardrobeRepo: &fakeWardrobeRepo{}, owns: false}
	h := newIDORHandler(repo)

	req := withUser(httptest.NewRequest(http.MethodPost,
		"/v1/wardrobe/items/victim-item/search", strings.NewReader(`{"brand":"Nike"}`)), "attacker")
	rr := httptest.NewRecorder()
	h.Search(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("non-owner search: want 404, got %d (%s)", rr.Code, rr.Body.String())
	}
	if repo.getImageCalled {
		t.Fatal("GetImage must not be reached for a non-owner (image would be exfiltrated)")
	}
}

// A different user must not be able to overwrite another user's item
// image via PATCH /items/{id}; the destructive GridFS write must not
// happen before the ownership check.
func TestUpdateItem_RejectsNonOwnerBeforeImageWrite(t *testing.T) {
	repo := &idorRepo{fakeWardrobeRepo: &fakeWardrobeRepo{}, owns: false}
	h := newIDORHandler(repo)

	req := withUser(httptest.NewRequest(http.MethodPatch, "/v1/wardrobe/items/victim-item",
		strings.NewReader(`{"traits":{"color":"black"},"imageUrl":"https://example.com/x.jpg"}`)), "attacker")
	rr := httptest.NewRecorder()
	h.updateItem(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("non-owner update: want 404, got %d (%s)", rr.Code, rr.Body.String())
	}
	if repo.saveImageCalled {
		t.Fatal("SaveImage must not be reached for a non-owner (victim image would be overwritten)")
	}
	if repo.updateItemCalled {
		t.Fatal("UpdateItem must not be reached for a non-owner")
	}
}

// The owner's update still works (traits-only, no image path).
func TestUpdateItem_OwnerSucceeds(t *testing.T) {
	repo := &idorRepo{fakeWardrobeRepo: &fakeWardrobeRepo{}, owns: true}
	h := newIDORHandler(repo)

	req := withUser(httptest.NewRequest(http.MethodPatch, "/v1/wardrobe/items/my-item",
		strings.NewReader(`{"traits":{"color":"black"}}`)), "owner")
	rr := httptest.NewRecorder()
	h.updateItem(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("owner update: want 200, got %d (%s)", rr.Code, rr.Body.String())
	}
	if !repo.updateItemCalled {
		t.Fatal("UpdateItem should be reached for the owner")
	}
}
