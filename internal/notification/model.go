package notification

import (
	"ai-business-api/internal/product"
	"ai-business-api/internal/user"
	"errors"
	"strings"
	"time"
)

var ErrNotFound = errors.New("notification or reminder not found")
var ErrConflict = errors.New("reminder already processed or limit reached")

type Notification struct {
	ID         string     `json:"id"`
	Kind       string     `json:"kind"`
	Title      string     `json:"title"`
	Body       string     `json:"body"`
	Href       string     `json:"href"`
	CreatedAt  time.Time  `json:"createdAt"`
	ReadAt     *time.Time `json:"readAt"`
	ResolvedAt *time.Time `json:"resolvedAt"`
}
type Reminder struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Notes       string     `json:"notes"`
	DueAt       time.Time  `json:"dueAt"`
	Status      string     `json:"status"`
	CreatedAt   time.Time  `json:"createdAt"`
	DeliveredAt *time.Time `json:"deliveredAt"`
}
type Input struct {
	Title string    `json:"title"`
	Notes string    `json:"notes"`
	DueAt time.Time `json:"dueAt"`
}

func Validate(in *Input, now time.Time) error {
	in.Title = strings.TrimSpace(in.Title)
	in.Notes = strings.TrimSpace(in.Notes)
	if in.Title == "" || !product.Text(in.Title, 200) || !product.Text(in.Notes, 2000) || in.DueAt.Before(now.Add(-time.Minute)) || in.DueAt.After(now.Add(366*24*time.Hour)) {
		return user.ErrInvalid
	}
	return nil
}
