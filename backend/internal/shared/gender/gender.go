// Package gender defines the canonical gender values shared across
// user profiles and clothing items, with validation helpers.
//
// User profiles are male/female today; clothing items and archetype
// defaults additionally allow "unisex". Keeping the values and the
// validation in one place means adding a future value (e.g. a
// non-binary option) is a single-file change.
package gender

const (
	// Male and Female apply to both user profiles and items.
	Male   = "male"
	Female = "female"
	// Unisex applies to clothing items and archetype defaults only —
	// a garment suitable for any user. It is NOT a valid user-profile
	// value; on the filter side a user's gender plus Unisex is what
	// participates in their moodboards.
	Unisex = "unisex"
)

// ValidUser reports whether s is an allowed user-profile gender.
func ValidUser(s string) bool {
	return s == Male || s == Female
}

// ValidItem reports whether s is an allowed gender for a clothing
// item or an archetype-default item (the user values plus Unisex).
func ValidItem(s string) bool {
	return s == Male || s == Female || s == Unisex
}
