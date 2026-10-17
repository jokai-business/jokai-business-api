package invoice

import (
	"ai-business-api/internal/quote"
	"errors"
	"time"
)

var ErrNotFound = errors.New("invoice not found")
var ErrConflict = errors.New("invoice changed or action not allowed")
var ErrExists = errors.New("quote already invoiced")
var ErrPayment = errors.New("payment exceeds balance or date invalid")

type Input struct {
	QuoteID   string `json:"quoteId"`
	ProjectID string `json:"projectId"`
	IssueDate string `json:"issueDate"`
	DueDate   string `json:"dueDate"`
	Tax       string `json:"tax"`
	Notes     string `json:"notes"`
}
type Patch struct {
	IssueDate string `json:"issueDate"`
	DueDate   string `json:"dueDate"`
	Tax       string `json:"tax"`
	Notes     string `json:"notes"`
	Version   int    `json:"version"`
}
type Action struct {
	Version int `json:"version"`
}
type Invoice struct {
	Input
	ID            string       `json:"id"`
	CustomerID    string       `json:"customerId"`
	Number        string       `json:"number"`
	QuoteNumber   string       `json:"quoteNumber"`
	CompanyName   string       `json:"companyName"`
	CustomerName  string       `json:"customerName"`
	CustomerEmail string       `json:"customerEmail"`
	Currency      string       `json:"currency"`
	Subtotal      string       `json:"subtotal"`
	Total         string       `json:"total"`
	Paid          string       `json:"paid"`
	Outstanding   string       `json:"outstanding"`
	State         string       `json:"state"`
	Status        string       `json:"status"`
	Version       int          `json:"version"`
	CreatedAt     time.Time    `json:"createdAt"`
	Items         []quote.Item `json:"items"`
	Payments      []Payment    `json:"payments"`
}
type PaymentInput struct {
	Amount         string `json:"amount"`
	PaymentDate    string `json:"paymentDate"`
	Reference      string `json:"reference"`
	Method         string `json:"method"`
	Notes          string `json:"notes"`
	IdempotencyKey string `json:"idempotencyKey"`
}
type Payment struct {
	PaymentInput
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"createdAt"`
}
type Balance struct {
	Currency     string `json:"currency"`
	Invoiced     string `json:"invoiced"`
	Collected    string `json:"collected"`
	Outstanding  string `json:"outstanding"`
	Overdue      string `json:"overdue"`
	OpenCount    int    `json:"openCount"`
	OverdueCount int    `json:"overdueCount"`
	DraftCount   int    `json:"draftCount"`
}
