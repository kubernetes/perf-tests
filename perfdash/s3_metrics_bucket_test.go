/*
Copyright The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func TestS3MetricsBucketGetBuildNumbersFollowsPagination(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("continuation-token") == "" {
			_, _ = w.Write([]byte(`<ListBucketResult><IsTruncated>true</IsTruncated><NextContinuationToken>page-2</NextContinuationToken><CommonPrefixes><Prefix>logs/job/1/</Prefix></CommonPrefixes></ListBucketResult>`))
			return
		}
		_, _ = w.Write([]byte(`<ListBucketResult><IsTruncated>false</IsTruncated><CommonPrefixes><Prefix>logs/job/2/</Prefix></CommonPrefixes></ListBucketResult>`))
	}))
	defer server.Close()

	client := s3.NewFromConfig(aws.Config{
		Region:       "us-west-2",
		Credentials:  credentials.NewStaticCredentialsProvider("test", "test", ""),
		BaseEndpoint: aws.String(server.URL),
	}, func(o *s3.Options) {
		o.UsePathStyle = true
	})
	bucket := &S3MetricsBucket{client: client, bucket: aws.String("test"), logPath: "logs"}

	builds, err := bucket.GetBuildNumbers("job")
	if err != nil {
		t.Fatalf("GetBuildNumbers() error = %v", err)
	}
	if want := []int{1, 2}; !reflect.DeepEqual(builds, want) {
		t.Errorf("GetBuildNumbers() = %v, want %v", builds, want)
	}
}

func TestS3MetricsBucketListFilesInBuildFollowsPagination(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("continuation-token") == "" {
			_, _ = w.Write([]byte(`<ListBucketResult><IsTruncated>true</IsTruncated><NextContinuationToken>page-2</NextContinuationToken><Contents><Key>logs/job/1/artifacts/first</Key></Contents></ListBucketResult>`))
			return
		}
		_, _ = w.Write([]byte(`<ListBucketResult><IsTruncated>false</IsTruncated><Contents><Key>logs/job/1/artifacts/second</Key></Contents></ListBucketResult>`))
	}))
	defer server.Close()

	client := s3.NewFromConfig(aws.Config{
		Region:       "us-west-2",
		Credentials:  credentials.NewStaticCredentialsProvider("test", "test", ""),
		BaseEndpoint: aws.String(server.URL),
	}, func(o *s3.Options) {
		o.UsePathStyle = true
	})
	bucket := &S3MetricsBucket{client: client, bucket: aws.String("test"), logPath: "logs"}

	files, err := bucket.ListFilesInBuild("job", 1, "artifacts")
	if err != nil {
		t.Fatalf("ListFilesInBuild() error = %v", err)
	}
	if want := []string{"logs/job/1/artifacts/first", "logs/job/1/artifacts/second"}; !reflect.DeepEqual(files, want) {
		t.Errorf("ListFilesInBuild() = %v, want %v", files, want)
	}
}
