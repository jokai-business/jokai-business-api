package document

import (
	"ai-business-api/internal/customer"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
)

// BlobStore keeps storage independent from tenant authorization in Service.
type BlobStore interface {
	Put(context.Context, string, string, []byte) error
	Get(context.Context, string, string) ([]byte, error)
}
type LocalStore struct{ Root string }

func (s LocalStore) path(org, id string) (string, error) {
	if !filepath.IsAbs(s.Root) || !customer.UUID(org) || !customer.UUID(id) {
		return "", errors.New("invalid storage key")
	}
	return filepath.Join(s.Root, org, id), nil
}
func (s LocalStore) Put(ctx context.Context, org, id string, data []byte) error {
	path, err := s.path(org, id)
	if err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		f.Close()
		if !ok {
			os.Remove(path)
		}
	}()
	if _, err = f.Write(data); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	ok = true
	return nil
}
func (s LocalStore) Get(ctx context.Context, org, id string) ([]byte, error) {
	path, err := s.path(org, id)
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > MaxSize {
		return nil, errors.New("invalid stored document")
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxSize {
		return nil, ErrSize
	}
	return data, nil
}
func (s LocalStore) Delete(ctx context.Context, org, id string) error {
	path, err := s.path(org, id)
	if err != nil {
		return err
	}
	return os.Remove(path)
}

// CheckHealth verifies the configured directory exists. It does not write test data
// or establish per-file read/write permission or available disk capacity.
func (s LocalStore) CheckHealth(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	info, err := os.Stat(s.Root)
	if err != nil || !info.IsDir() {
		return ErrStorageUnavailable
	}
	return nil
}
