package opportunity

import (
	"errors"
	"time"
)

var ErrNotFound = errors.New("opportunity or stage not found")
var ErrConflict = errors.New("record changed")

type Input struct {
	CustomerID        string `json:"customerId"`
	Title             string `json:"title"`
	Requirement       string `json:"requirement"`
	StageID           string `json:"stageId"`
	ExpectedValue     string `json:"expectedValue"`
	Currency          string `json:"currency"`
	ExpectedCloseDate string `json:"expectedCloseDate"`
	OwnerID           string `json:"ownerId"`
	Source            string `json:"source"`
	NextAction        string `json:"nextAction"`
	Probability       int    `json:"probability"`
}
type Patch struct {
	CustomerID        *string `json:"customerId"`
	Title             *string `json:"title"`
	Requirement       *string `json:"requirement"`
	StageID           *string `json:"stageId"`
	ExpectedValue     *string `json:"expectedValue"`
	Currency          *string `json:"currency"`
	ExpectedCloseDate *string `json:"expectedCloseDate"`
	OwnerID           *string `json:"ownerId"`
	Source            *string `json:"source"`
	NextAction        *string `json:"nextAction"`
	Probability       *int    `json:"probability"`
	Version           int     `json:"version"`
}
type Opportunity struct {
	Input
	ID           string    `json:"id"`
	CustomerName string    `json:"customerName"`
	OwnerName    string    `json:"ownerName"`
	StageName    string    `json:"stageName"`
	IsClosed     bool      `json:"isClosed"`
	IsWon        bool      `json:"isWon"`
	Version      int       `json:"version"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}
type StageInput struct {
	Name     string `json:"name"`
	Position int    `json:"position"`
	IsClosed bool   `json:"isClosed"`
	IsWon    bool   `json:"isWon"`
	Version  int    `json:"version"`
}
type Stage struct {
	StageInput
	ID string `json:"id"`
}
type Activity struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	Body      string    `json:"body"`
	ActorName string    `json:"actorName"`
	CreatedAt time.Time `json:"createdAt"`
}
