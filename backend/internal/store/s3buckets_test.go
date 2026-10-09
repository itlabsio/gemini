package store

import (
	"errors"
	"testing"
)

func TestS3BucketSecretRoundTrip(t *testing.T) {
	s, ctx := testStore(t)

	in := S3BucketInput{Name: "rt-bucket", Endpoint: "s3.example", Bucket: "dumps",
		AccessKeyID: "AK", SecretAccessKey: "SK1", PathStyle: true, UseSSL: true, Enabled: true}
	b, err := s.CreateS3Bucket(ctx, in, "tester@example.com")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _ = s.DeleteS3Bucket(ctx, b.ID) })

	if _, err := s.CreateS3Bucket(ctx, in, ""); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate name: want ErrConflict, got %v", err)
	}

	secretOf := func() string {
		t.Helper()
		all, err := s.EnabledS3Buckets(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, x := range all {
			if x.ID == b.ID {
				return x.SecretAccessKey
			}
		}
		return ""
	}
	if got := secretOf(); got != "SK1" {
		t.Fatalf("secret after create = %q", got)
	}

	// пустой секрет при update — оставить сохранённый
	in.SecretAccessKey = ""
	in.Bucket = "dumps2"
	if _, err := s.UpdateS3Bucket(ctx, b.ID, in); err != nil {
		t.Fatalf("update: %v", err)
	}
	if got := secretOf(); got != "SK1" {
		t.Fatalf("secret after keep-update = %q", got)
	}

	// выключенный бакет в EnabledS3Buckets не попадает
	in.Enabled = false
	if _, err := s.UpdateS3Bucket(ctx, b.ID, in); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if got := secretOf(); got != "" {
		t.Fatal("disabled bucket is still listed as enabled")
	}
}
