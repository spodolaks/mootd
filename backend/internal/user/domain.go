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
	CreatedAt        string             `bson:"createdAt"                    json:"createdAt"`
	UpdatedAt        string             `bson:"updatedAt"                    json:"updatedAt"`
}

// UpdateProfileRequest is the request body for PUT /v1/user/profile.
// Both fields are optional; at least one must be supplied.
type UpdateProfileRequest struct {
	Name      *string `json:"name,omitempty"`
	AvatarURL *string `json:"avatarUrl,omitempty"`
}
