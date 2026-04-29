package admin

import (
	"context"
	"regexp"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// SearchKind enumerates the result types this admin search returns.
// Today only "user" is wired; "trace", "prompt", "audit" are planned.
// The frontend dispatches on this field so adding new kinds is
// additive.
type SearchKind string

const (
	SearchKindUser SearchKind = "user"
)

// SearchHit is one cross-collection result.
type SearchHit struct {
	Kind     SearchKind `json:"kind"`
	ID       string     `json:"id"`
	Title    string     `json:"title"`
	Subtitle string     `json:"subtitle,omitempty"`
}

// SearchResponse wraps a flat list of hits. We keep the list flat
// rather than grouping-by-kind so the frontend can render a single
// virtual list and the cmdk fuzzy match still works cleanly.
type SearchResponse struct {
	Hits []SearchHit `json:"hits"`
}

// SearchRepository is the read surface for /admin/v1/search.
type SearchRepository interface {
	// SearchUsers does a case-insensitive contains-match on the email
	// column. Returns up to maxHits results. Empty input returns an
	// empty slice (not an error) so caller-side debouncing is
	// forgiving.
	SearchUsers(ctx context.Context, query string, maxHits int) ([]SearchHit, error)
}

// searchUsers shares the implementation between the users repo (for
// callers that already have a UsersMongoRepository) and any future
// search-specific repo. Kept as a free function so we don't need a
// separate type.
func searchUsers(ctx context.Context, col *mongo.Collection, query string, maxHits int) ([]SearchHit, error) {
	if maxHits <= 0 || maxHits > 50 {
		maxHits = 10
	}
	if query == "" {
		return []SearchHit{}, nil
	}

	// Escape regex meta-characters so a query like "foo+bar" doesn't
	// turn into a regex with quantifier semantics. \Q…\E would be
	// nice but Mongo's PCRE2 dialect handles QuoteMeta-style escapes
	// fine.
	pattern := regexp.QuoteMeta(query)

	cur, err := col.Find(ctx, bson.M{"email": bson.M{
		"$regex":   pattern,
		"$options": "i",
	}}, findOpts().
		SetProjection(bson.M{"_id": 1, "email": 1, "name": 1}).
		SetLimit(int64(maxHits)))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	var rows []struct {
		ID    string `bson:"_id"`
		Email string `bson:"email"`
		Name  string `bson:"name,omitempty"`
	}
	if err := cur.All(ctx, &rows); err != nil {
		return nil, err
	}

	hits := make([]SearchHit, 0, len(rows))
	for _, r := range rows {
		hits = append(hits, SearchHit{
			Kind:     SearchKindUser,
			ID:       r.ID,
			Title:    r.Email,
			Subtitle: r.Name,
		})
	}
	return hits, nil
}

// SearchUsers on UsersMongoRepository — exposes the helper so the
// handler can reuse the same Mongo client / db config the users
// list already uses. Doesn't introduce a new Repository to wire.
func (r *UsersMongoRepository) SearchUsers(ctx context.Context, query string, maxHits int) ([]SearchHit, error) {
	return searchUsers(ctx, r.usersCol(), query, maxHits)
}
