package quote

import (
	"errors"
	"time"
)

var ErrNotFound = errors.New("quote not found")
var ErrConflict = errors.New("quote changed or transition is not allowed")
var ErrExpired = errors.New("quote expired")

type ItemInput struct {
	ProductID       string `json:"productId"`
	Quantity        string `json:"quantity"`
	UnitPrice       string `json:"unitPrice"`
	DiscountPercent string `json:"discountPercent"`
}
type Item struct {
	ItemInput
	Name           string `json:"name"`
	Description    string `json:"description"`
	Unit           string `json:"unit"`
	Subtotal       string `json:"subtotal"`
	DiscountAmount string `json:"discountAmount"`
	Total          string `json:"total"`
}
type Input struct {
	OpportunityID string      `json:"opportunityId"`
	Currency      string      `json:"currency"`
	ExpiryDate    string      `json:"expiryDate"`
	Terms         string      `json:"terms"`
	Items         []ItemInput `json:"items"`
	Version       int         `json:"version"`
}
type Quote struct {
	ID               string     `json:"id"`
	CustomerID       string     `json:"customerId"`
	OpportunityID    string     `json:"opportunityId"`
	CreatorID        string     `json:"creatorId"`
	Number           string     `json:"number"`
	CompanyName      string     `json:"companyName"`
	CustomerName     string     `json:"customerName"`
	CustomerEmail    string     `json:"customerEmail"`
	OpportunityTitle string     `json:"opportunityTitle"`
	Currency         string     `json:"currency"`
	Status           string     `json:"status"`
	ExpiryDate       string     `json:"expiryDate"`
	Terms            string     `json:"terms"`
	Subtotal         string     `json:"subtotal"`
	DiscountTotal    string     `json:"discountTotal"`
	Total            string     `json:"total"`
	Version          int        `json:"version"`
	CreatedAt        time.Time  `json:"createdAt"`
	UpdatedAt        time.Time  `json:"updatedAt"`
	Items            []Item     `json:"items"`
	Approvals        []Approval `json:"approvals"`
}
type Approval struct {
	ID          string     `json:"id"`
	Status      string     `json:"status"`
	RequestedBy string     `json:"requestedBy"`
	DecidedBy   string     `json:"decidedBy"`
	Note        string     `json:"note"`
	RequestedAt time.Time  `json:"requestedAt"`
	DecidedAt   *time.Time `json:"decidedAt"`
}
type Action struct {
	Version int    `json:"version"`
	Note    string `json:"note"`
}
