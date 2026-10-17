package document

import (
	"context"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"io"
	"strings"
	"testing"
)

type objectAPI struct {
	size *int64
	body string
}

func (f objectAPI) PutObject(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	return &s3.PutObjectOutput{}, nil
}
func (f objectAPI) GetObject(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	return &s3.GetObjectOutput{ContentLength: f.size, Body: io.NopCloser(strings.NewReader(f.body))}, nil
}
func TestS3ReadBound(t *testing.T) {
	for _, f := range []objectAPI{{size: aws.Int64(MaxSize + 1)}, {body: strings.Repeat("a", MaxSize+1)}} {
		s := S3Store{Client: f, Bucket: "bucket"}
		if _, err := s.Get(context.Background(), "10000000-0000-0000-0000-000000000001", "20000000-0000-0000-0000-000000000001"); err != ErrSize {
			t.Fatal(err)
		}
	}
}
func TestStorageRouting(t *testing.T) {
	local := LocalStore{Root: t.TempDir()}
	primary := S3Store{Client: objectAPI{}, Bucket: "bucket"}
	s := Service{Blobs: primary, StorageID: "s3:current", LegacyBlobs: local}
	got, err := s.storeFor("local")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got.(LocalStore); !ok {
		t.Fatal("legacy file redirected")
	}
	got, err = s.storeFor("s3:current")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got.(S3Store); !ok {
		t.Fatal("new file redirected")
	}
	if _, err = s.storeFor("s3:different"); err == nil {
		t.Fatal("unknown location fell back")
	}
	s = Service{Blobs: local}
	if _, err = s.storeFor("s3:current"); err == nil {
		t.Fatal("S3 file redirected to local storage")
	}
}
