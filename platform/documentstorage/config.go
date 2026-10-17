package documentstorage

import (
	"ai-business-api/internal/document"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type Settings struct {
	Backend, LocalPath, Endpoint, Bucket, Region, AccessKey, SecretKey, SessionToken string
	PathStyle, AllowHTTP                                                             bool
}

func Read(getenv func(string) string) (Settings, error) {
	c := Settings{Backend: getenv("DOCUMENT_STORAGE_BACKEND"), LocalPath: getenv("DOCUMENT_STORAGE_PATH"), Endpoint: getenv("DOCUMENT_S3_ENDPOINT"), Bucket: getenv("DOCUMENT_S3_BUCKET"), Region: getenv("DOCUMENT_S3_REGION"), AccessKey: getenv("DOCUMENT_S3_ACCESS_KEY_ID"), SecretKey: getenv("DOCUMENT_S3_SECRET_ACCESS_KEY"), SessionToken: getenv("DOCUMENT_S3_SESSION_TOKEN")}
	if c.Backend == "" {
		c.Backend = "local"
	}
	if c.Backend != "local" && c.Backend != "s3" {
		return c, errors.New("DOCUMENT_STORAGE_BACKEND must be local or s3")
	}
	// Retain local access when new uploads use S3; existing local records are never reinterpreted.
	if !filepath.IsAbs(c.LocalPath) {
		return c, errors.New("DOCUMENT_STORAGE_PATH must be an absolute private directory")
	}
	if c.Backend == "local" {
		return c, nil
	}
	if !regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`).MatchString(c.Bucket) || strings.Contains(c.Bucket, "..") || c.Region == "" {
		return c, errors.New("valid DOCUMENT_S3_BUCKET and DOCUMENT_S3_REGION are required")
	}
	for name, dest := range map[string]*bool{"DOCUMENT_S3_PATH_STYLE": &c.PathStyle, "DOCUMENT_S3_ALLOW_HTTP": &c.AllowHTTP} {
		if v := getenv(name); v != "" {
			b, e := strconv.ParseBool(v)
			if e != nil {
				return c, errors.New("invalid document storage boolean")
			}
			*dest = b
		}
	}
	if (c.AccessKey == "") != (c.SecretKey == "") || (c.SessionToken != "" && c.AccessKey == "") {
		return c, errors.New("supply both document S3 access and secret keys, or use the AWS credential chain")
	}
	if c.Endpoint != "" {
		u, e := url.Parse(c.Endpoint)
		if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || (u.Scheme != "https" && !(u.Scheme == "http" && c.AllowHTTP)) {
			return c, errors.New("DOCUMENT_S3_ENDPOINT must be an HTTPS origin; HTTP requires explicit local-development opt-in")
		}
		c.Endpoint = strings.TrimSuffix(c.Endpoint, "/")
	}
	return c, nil
}
func (c Settings) ID() string {
	if c.Backend == "local" {
		return "local"
	}
	v := sha256.Sum256([]byte(c.Endpoint + "\n" + c.Region + "\n" + c.Bucket + "\ndocuments/"))
	return "s3:" + hex.EncodeToString(v[:])
}
func Open(ctx context.Context, c Settings) (document.BlobStore, error) {
	if c.Backend == "local" {
		return document.LocalStore{Root: c.LocalPath}, nil
	}
	opts := []func(*config.LoadOptions) error{config.WithRegion(c.Region), config.WithHTTPClient(&http.Client{Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}), config.WithRetryMaxAttempts(2)}
	if c.AccessKey != "" {
		opts = append(opts, config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(c.AccessKey, c.SecretKey, c.SessionToken)))
	}
	cfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, errors.New("could not initialize document S3 credentials")
	}
	// Only the dedicated document endpoint may change storage routing.
	cfg.BaseEndpoint = nil
	cfg.ConfigSources = nil
	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.UsePathStyle = c.PathStyle
		if c.Endpoint != "" {
			o.BaseEndpoint = aws.String(c.Endpoint)
		}
	})
	check, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	if _, err = client.HeadBucket(check, &s3.HeadBucketInput{Bucket: aws.String(c.Bucket)}); err != nil {
		return nil, errors.New("document S3 bucket is unavailable or credentials lack access")
	}
	return document.S3Store{Client: client, Bucket: c.Bucket}, nil
}
