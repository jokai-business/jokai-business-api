package user

import "errors"

var ErrForbidden = errors.New("access denied")
var ErrInvalid = errors.New("invalid input")
var ErrConflict = errors.New("user already exists")

type Organization struct {
	DefaultCurrency string `json:"defaultCurrency"`
	Timezone        string `json:"timezone"`
	ID              string `json:"id"`
	Name            string `json:"name"`
}
type User struct {
	ID    string   `json:"id"`
	Name  string   `json:"name"`
	Email string   `json:"email"`
	Roles []string `json:"roles"`
}
type Context struct {
	User         User         `json:"user"`
	Organization Organization `json:"organization"`
	Permissions  []string     `json:"permissions"`
}

func (c Context) Allows(permission string) bool {
	for _, p := range c.Permissions {
		if p == permission {
			return true
		}
	}
	return false
}

type CreateRequest struct {
	Subject string `json:"subject"`
	Name    string `json:"name"`
	Email   string `json:"email"`
	Role    string `json:"role"`
}
type Audit struct {
	ID         string `json:"id"`
	Action     string `json:"action"`
	ActorID    string `json:"actorId"`
	ResourceID string `json:"resourceId"`
	CreatedAt  string `json:"createdAt"`
	RequestID  string `json:"requestId"`
}
