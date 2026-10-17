// s3check exercises the real adapter against a disposable loopback-only fixture.
// It deliberately accepts no provider endpoint or production credentials.
package main

import (
	"ai-business-api/platform/documentstorage"
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
)

const endpoint = "https://127.0.0.1:9000"
const bucket = "disposable-document-check"
const access = "fixture-access-only"
const secret = "fixture-secret-only-not-for-production"
const org = "10000000-0000-0000-0000-000000000001"
const other = "10000000-0000-0000-0000-000000000002"
const id = "20000000-0000-0000-0000-000000000001"

func check() error {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	client := &http.Client{Timeout: 2 * time.Second}
	ready := false
	for i := 0; i < 30; i++ {
		response, err := client.Get(endpoint + "/minio/health/live")
		if err == nil {
			response.Body.Close()
			if response.StatusCode == 200 {
				ready = true
				break
			}
		}
		time.Sleep(time.Second)
	}
	if !ready {
		return errors.New("fixture readiness failed")
	}
	api := s3.New(s3.Options{Region: "us-east-1", BaseEndpoint: aws.String(endpoint), UsePathStyle: true,
		Credentials: credentials.NewStaticCredentialsProvider(access, secret, "")})
	if _, err := api.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket)}); err != nil {
		return errors.New("fixture bucket creation failed")
	}
	settings := documentstorage.Settings{Backend: "s3", LocalPath: "/unused", Endpoint: endpoint, Bucket: bucket, Region: "us-east-1", AccessKey: access, SecretKey: secret, PathStyle: true, AllowHTTP: false}
	store, err := documentstorage.Open(ctx, settings)
	if err != nil {
		return errors.New("adapter startup health check failed")
	}
	original := []byte("%PDF-1.4\nSynthetic storage conformance document\n%%EOF")
	if store.Put(ctx, org, id, original) != nil {
		return errors.New("upload failed")
	}
	got, err := store.Get(ctx, org, id)
	if err != nil || !bytes.Equal(got, original) {
		return errors.New("original download mismatch")
	}
	err = store.Put(ctx, org, id, []byte("replacement must not overwrite"))
	var failure smithy.APIError
	if !errors.As(err, &failure) || failure.ErrorCode() != "PreconditionFailed" {
		return errors.New("overwrite was not rejected with a precondition failure")
	}
	got, err = store.Get(ctx, org, id)
	if err != nil || !bytes.Equal(got, original) {
		return errors.New("overwrite attempt changed original")
	}
	if _, err = store.Get(ctx, other, id); err == nil {
		return errors.New("organization key separation failed")
	}
	if store.Put(ctx, other, id, []byte("other organization fixture")) != nil {
		return errors.New("other organization upload failed")
	}
	got, err = store.Get(ctx, org, id)
	if err != nil || !bytes.Equal(got, original) {
		return errors.New("organization original changed")
	}
	// Eight simultaneous first writes must produce one winner and seven 412s.
	raceID := "20000000-0000-0000-0000-000000000002"
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); results <- store.Put(ctx, org, raceID, original) }()
	}
	wg.Wait()
	close(results)
	winners := 0
	for err := range results {
		if err == nil {
			winners++
			continue
		}
		var apiErr smithy.APIError
		if !errors.As(err, &apiErr) || apiErr.ErrorCode() != "PreconditionFailed" {
			return errors.New("concurrent write returned unexpected error")
		}
	}
	if winners != 1 {
		return errors.New("concurrent creation did not preserve one original")
	}
	got, err = store.Get(ctx, org, raceID)
	if err != nil || !bytes.Equal(got, original) {
		return errors.New("concurrent original download mismatch")
	}
	settings.SecretKey = "invalid-fixture-secret"
	if _, err = documentstorage.Open(ctx, settings); err == nil {
		return errors.New("invalid credentials were accepted")
	}
	settings.SecretKey = secret
	settings.Bucket = "missing-fixture-bucket"
	if _, err = documentstorage.Open(ctx, settings); err == nil {
		return errors.New("missing bucket was accepted")
	}
	got, err = store.Get(ctx, org, id)
	if err != nil || !bytes.Equal(got, original) {
		return errors.New("valid access failed after negative checks")
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"passed": true, "checks": []string{"startup-bucket-check", "upload-download", "overwrite-refusal", "original-preserved", "organization-key-separation", "concurrent-first-write", "invalid-credentials", "missing-bucket"}, "concurrentWrites": 8, "successfulFirstWrites": winners})
}

// Each negative case runs in a fresh process so the system trust pool is not cached.
func checkTLSRejection(mode string) error {
	target := endpoint
	if mode == "hostname" {
		target = "https://127.0.0.2:9000"
	}
	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Get(target + "/minio/health/live")
	if response != nil {
		response.Body.Close()
	}
	var unknown x509.UnknownAuthorityError
	var hostname x509.HostnameError
	if (mode == "untrusted" && !errors.As(err, &unknown)) || (mode == "hostname" && !errors.As(err, &hostname)) {
		return errors.New("expected certificate verification failure was not observed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	settings := documentstorage.Settings{Backend: "s3", LocalPath: "/unused", Endpoint: target, Bucket: bucket, Region: "us-east-1", AccessKey: access, SecretKey: secret, PathStyle: true}
	if _, err := documentstorage.Open(ctx, settings); err == nil {
		return errors.New("adapter accepted invalid TLS certificate")
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"passed": true, "check": "tls-" + mode + "-rejected"})
}

func main() {
	var err error
	switch {
	case len(os.Args) == 1:
		err = check()
	case len(os.Args) == 2 && (os.Args[1] == "untrusted" || os.Args[1] == "hostname"):
		err = checkTLSRejection(os.Args[1])
	default:
		err = errors.New("unsupported fixture check mode")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}
