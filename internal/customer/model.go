package customer

import (
	"errors"
	"time"
)

var ErrNotFound = errors.New("customer not found")
var ErrConflict = errors.New("customer changed")

type Customer struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	Phone     string    `json:"phone"`
	Website   string    `json:"website"`
	Industry  string    `json:"industry"`
	Country   string    `json:"country"`
	OwnerID   string    `json:"ownerId"`
	OwnerName string    `json:"ownerName"`
	Status    string    `json:"status"`
	Notes     string    `json:"notes"`
	Version   int       `json:"version"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}
type Input struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Phone    string `json:"phone"`
	Website  string `json:"website"`
	Industry string `json:"industry"`
	Country  string `json:"country"`
	OwnerID  string `json:"ownerId"`
	Status   string `json:"status"`
	Notes    string `json:"notes"`
}
type Patch struct {
	Name     *string `json:"name"`
	Email    *string `json:"email"`
	Phone    *string `json:"phone"`
	Website  *string `json:"website"`
	Industry *string `json:"industry"`
	Country  *string `json:"country"`
	OwnerID  *string `json:"ownerId"`
	Status   *string `json:"status"`
	Notes    *string `json:"notes"`
	Version  int     `json:"version"`
}
type Contact struct {
	ID         string    `json:"id"`
	CustomerID string    `json:"customerId"`
	Name       string    `json:"name"`
	Email      string    `json:"email"`
	Phone      string    `json:"phone"`
	Title      string    `json:"title"`
	CreatedAt  time.Time `json:"createdAt"`
}
type ContactInput struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	Phone string `json:"phone"`
	Title string `json:"title"`
}
type Owner struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
