package document

import (
	"context"
	"errors"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"os"
	"path/filepath"
	"testing"
)

type healthS3 struct {
	objectAPI
	bucket string
	err    error
}

func (s *healthS3) HeadBucket(_ context.Context, in *s3.HeadBucketInput, _ ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
	s.bucket = *in.Bucket
	return &s3.HeadBucketOutput{}, s.err
}
func TestStorageHealth(t *testing.T) {
	dir := t.TempDir()
	store := LocalStore{Root: dir}
	if store.CheckHealth(context.Background()) != nil {
		t.Fatal("directory unavailable")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatal("health wrote files")
	}
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{file, filepath.Join(dir, "missing")} {
		if (LocalStore{Root: root}).CheckHealth(context.Background()) == nil {
			t.Fatal("invalid storage root")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if store.CheckHealth(ctx) == nil {
		t.Fatal("ignored cancellation")
	}
	client := &healthS3{}
	remote := S3Store{Client: client, Bucket: "configured-bucket"}
	if remote.CheckHealth(context.Background()) != nil || client.bucket != "configured-bucket" {
		t.Fatal("bucket not checked")
	}
	client.err = errors.New("access denied")
	if remote.CheckHealth(context.Background()) == nil {
		t.Fatal("denial ignored")
	}
}
