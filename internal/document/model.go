package document

import (
	"errors"
	"time"
)

const MaxSize = 10 << 20

var ErrNotFound = errors.New("document not found")
var ErrType = errors.New("unsupported document")
var ErrSize = errors.New("document too large")
var ErrStorageUnavailable = errors.New("document storage is unavailable")
var ErrConflict = errors.New("document changed")

type Document struct {
	StorageID    string     `json:"-"`
	ID           string     `json:"id"`
	Title        string     `json:"title"`
	Workspace    string     `json:"workspace"`
	CustomerID   string     `json:"customerId"`
	CustomerName string     `json:"customerName"`
	Filename     string     `json:"filename"`
	ContentType  string     `json:"contentType"`
	SizeBytes    int64      `json:"sizeBytes"`
	SHA256       string     `json:"sha256"`
	Notes        string     `json:"notes"`
	Status       string     `json:"status"`
	UploadedBy   string     `json:"uploadedBy"`
	UploaderName string     `json:"uploaderName"`
	ReviewedBy   string     `json:"reviewedBy"`
	ReviewerName string     `json:"reviewerName"`
	ReviewNote   string     `json:"reviewNote"`
	Version      int        `json:"version"`
	CreatedAt    time.Time  `json:"createdAt"`
	ReviewedAt   *time.Time `json:"reviewedAt"`
}
type Input struct{ Title, Workspace, CustomerID, Notes, Filename string }
type Review struct {
	Version int    `json:"version"`
	Status  string `json:"status"`
	Note    string `json:"note"`
}
