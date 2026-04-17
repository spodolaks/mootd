package pagination

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// Cursor represents a position in cursor-based pagination.
type Cursor struct {
	CreatedAt time.Time `json:"c"`
	ID        string    `json:"i"`
}

// EncodeCursor serialises createdAt + id into a URL-safe base64 string.
func EncodeCursor(createdAt time.Time, id string) string {
	c := Cursor{CreatedAt: createdAt, ID: id}
	data, _ := json.Marshal(c)
	return base64.URLEncoding.EncodeToString(data)
}

// DecodeCursor parses a cursor string produced by EncodeCursor.
func DecodeCursor(s string) (*Cursor, error) {
	data, err := base64.URLEncoding.DecodeString(s)
	if err != nil {
		return nil, err
	}
	var c Cursor
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

// ParseParams extracts limit and cursor from query params.
// defaultLimit is used when limit is not specified. maxLimit caps the value.
func ParseParams(r *http.Request, defaultLimit, maxLimit int) (int, *Cursor) {
	limit := defaultLimit
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > maxLimit {
		limit = maxLimit
	}

	var cursor *Cursor
	if c := r.URL.Query().Get("cursor"); c != "" {
		cursor, _ = DecodeCursor(c)
	}
	return limit, cursor
}

// BuildFilter adds cursor conditions to an existing MongoDB filter.
// Items are sorted by createdAt DESC, _id DESC — the cursor selects items
// "older" than the last seen item.
func BuildFilter(filter bson.M, cursor *Cursor) bson.M {
	if cursor == nil {
		return filter
	}
	filter["$or"] = bson.A{
		bson.M{"createdAt": bson.M{"$lt": cursor.CreatedAt}},
		bson.M{"createdAt": cursor.CreatedAt, "_id": bson.M{"$lt": cursor.ID}},
	}
	return filter
}
