// Package user handles user profile management.
package user

// UserDocument represents a user record in MongoDB.
type UserDocument struct {
	ID               string             `bson:"_id"                          json:"id"`
	Email            string             `bson:"email"                        json:"email"`
	Name             string             `bson:"name"                         json:"name"`
	AvatarURL        string             `bson:"avatarUrl,omitempty"          json:"avatarUrl,omitempty"`
	GoogleID         string             `bson:"googleId"                     json:"googleId,omitempty"`
	ArchetypeProfile map[string]float64 `bson:"archetypeProfile,omitempty"   json:"archetypeProfile,omitempty"`
	// Creativity is the user's outfit-generation variance
	// preference (mootd#67), 0..1. 0 = predictable / play-it-safe,
	// 1 = surprise me. Missing field treated as the default 0.5
	// at read time. The outfit service translates to a provider
	// temperature via outfit.CreativityToTemperature.
	Creativity *float64 `bson:"creativity,omitempty"          json:"creativity,omitempty"`
	CreatedAt        string             `bson:"createdAt"                    json:"createdAt"`
	UpdatedAt        string             `bson:"updatedAt"                    json:"updatedAt"`
}

// UpdateProfileRequest is the request body for PUT /v1/user/profile.
// Every field is optional; at least one must be supplied.
type UpdateProfileRequest struct {
	Name      *string  `json:"name,omitempty"`
	AvatarURL *string  `json:"avatarUrl,omitempty"`
	// Creativity is clamped to [0, 1] in the handler
	// (mootd#67). Pass *float64 so a caller can deliberately
	// reset to 0 (the slider's left-end "predictable" extreme)
	// without it being indistinguishable from "leave unchanged".
	Creativity *float64 `json:"creativity,omitempty"`
}
