// Package gender defines the canonical gender values shared across
// user profiles and clothing items, with validation helpers.
//
// All three values — male, female, unisex — are valid for both a
// user profile and an item. On a user, "unisex" means "no
// preference" ("as long as it's stylish") and the outfit-filler
// filter leaves them unrestricted; on an item or archetype default
// it means the garment suits any user. Keeping the values and
// validation in one place means adding a future value is a
// single-file change.
package gender

const (
	// Male and Female apply to both user profiles and items.
	Male   = "male"
	Female = "female"
	// Unisex on an item or archetype default means the garment suits
	// any user. On a user profile it means "no preference" — their
	// moodboards pull fillers of every gender.
	Unisex = "unisex"
)

// ValidUser reports whether s is an allowed user-profile gender —
// "male", "female", or "unisex" (the no-preference / "as long as
// it's stylish" option).
func ValidUser(s string) bool {
	return s == Male || s == Female || s == Unisex
}

// ValidItem reports whether s is an allowed gender for a clothing
// item or an archetype-default item (the user values plus Unisex).
func ValidItem(s string) bool {
	return s == Male || s == Female || s == Unisex
}
