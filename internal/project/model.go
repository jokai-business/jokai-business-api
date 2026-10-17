package project

import (
	"errors"
	"time"
)

var ErrNotFound = errors.New("project or task not found")
var ErrConflict = errors.New("record changed")
var ErrExists = errors.New("project already exists for opportunity")
var ErrNotEligible = errors.New("source must be won or accepted")
var ErrIncomplete = errors.New("complete all tasks before completing project")
var ErrClosed = errors.New("reopen project before changing tasks")

type Input struct {
	OpportunityID string `json:"opportunityId"`
	QuoteID       string `json:"quoteId"`
	Name          string `json:"name"`
	OwnerID       string `json:"ownerId"`
	StartDate     string `json:"startDate"`
	TargetDate    string `json:"targetDate"`
	Notes         string `json:"notes"`
}
type Patch struct {
	Name       *string `json:"name"`
	OwnerID    *string `json:"ownerId"`
	StartDate  *string `json:"startDate"`
	TargetDate *string `json:"targetDate"`
	Notes      *string `json:"notes"`
	Status     *string `json:"status"`
	Version    int     `json:"version"`
}
type Project struct {
	Input
	ID               string    `json:"id"`
	CustomerID       string    `json:"customerId"`
	CustomerName     string    `json:"customerName"`
	OpportunityTitle string    `json:"opportunityTitle"`
	QuoteNumber      string    `json:"quoteNumber"`
	OwnerName        string    `json:"ownerName"`
	Status           string    `json:"status"`
	TaskCount        int       `json:"taskCount"`
	DoneCount        int       `json:"doneCount"`
	Progress         int       `json:"progress"`
	Version          int       `json:"version"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}
type TaskInput struct {
	Title      string `json:"title"`
	AssigneeID string `json:"assigneeId"`
	DueDate    string `json:"dueDate"`
	Status     string `json:"status"`
	Priority   string `json:"priority"`
	Notes      string `json:"notes"`
}
type TaskPatch struct {
	Title      *string `json:"title"`
	AssigneeID *string `json:"assigneeId"`
	DueDate    *string `json:"dueDate"`
	Status     *string `json:"status"`
	Priority   *string `json:"priority"`
	Notes      *string `json:"notes"`
	Version    int     `json:"version"`
}
type Task struct {
	TaskInput
	ID           string    `json:"id"`
	ProjectID    string    `json:"projectId"`
	AssigneeName string    `json:"assigneeName"`
	Version      int       `json:"version"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}
type Source struct {
	OpportunityID    string `json:"opportunityId"`
	OpportunityTitle string `json:"opportunityTitle"`
	CustomerID       string `json:"customerId"`
	CustomerName     string `json:"customerName"`
	QuoteID          string `json:"quoteId"`
	QuoteNumber      string `json:"quoteNumber"`
	IsWon            bool   `json:"isWon"`
}
