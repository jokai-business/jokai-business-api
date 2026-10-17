package scope

import (
	"errors"
	"time"
)

var ErrNotFound = errors.New("scope not found")
var ErrConflict = errors.New("scope changed or action unavailable")
var ErrExists = errors.New("opportunity already has a scope")
var ErrIncomplete = errors.New("scope needs objectives and complete contributions")
var ErrSelfReview = errors.New("independent reviewer required")

type Input struct {
	OpportunityID string `json:"opportunityId"`
	Title         string `json:"title"`
	Objectives    string `json:"objectives"`
}
type Edit struct {
	Title      string `json:"title"`
	Objectives string `json:"objectives"`
	Version    int    `json:"version"`
}
type Action struct {
	Version int    `json:"version"`
	Note    string `json:"note"`
}
type ContributionInput struct {
	Team         string `json:"team"`
	Service      string `json:"service"`
	Deliverables string `json:"deliverables"`
	Assumptions  string `json:"assumptions"`
	Exclusions   string `json:"exclusions"`
	Timeline     string `json:"timeline"`
	Version      int    `json:"version"`
}
type Contribution struct {
	ContributionInput
	ID            string    `json:"id"`
	UpdatedBy     string    `json:"updatedBy"`
	UpdatedByName string    `json:"updatedByName"`
	UpdatedAt     time.Time `json:"updatedAt"`
}
type Scope struct {
	PreparedBy []string `json:"preparedBy"`
	Input
	ID                string         `json:"id"`
	CustomerID        string         `json:"customerId"`
	CustomerName      string         `json:"customerName"`
	OpportunityTitle  string         `json:"opportunityTitle"`
	CreatorID         string         `json:"creatorId"`
	EditedBy          string         `json:"editedBy"`
	CreatorName       string         `json:"creatorName"`
	SubmittedBy       string         `json:"submittedBy"`
	Status            string         `json:"status"`
	Revision          int            `json:"revision"`
	Version           int            `json:"version"`
	ContributionCount int            `json:"contributionCount"`
	CreatedAt         time.Time      `json:"createdAt"`
	UpdatedAt         time.Time      `json:"updatedAt"`
	Contributions     []Contribution `json:"contributions"`
}
type Review struct {
	ID        string    `json:"id"`
	Revision  int       `json:"revision"`
	Action    string    `json:"action"`
	Note      string    `json:"note"`
	ActorName string    `json:"actorName"`
	CreatedAt time.Time `json:"createdAt"`
}
type Revision struct {
	Revision   int       `json:"revision"`
	ApprovedBy string    `json:"approvedBy"`
	ApprovedAt time.Time `json:"approvedAt"`
}
