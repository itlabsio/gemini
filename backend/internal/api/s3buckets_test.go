package api

import "testing"

func TestS3BucketRequestInput(t *testing.T) {
	ok := s3BucketRequest{Name: " dr ", Endpoint: "storage.yandexcloud.net/", Bucket: "b", AccessKeyID: "k", SecretAccessKey: "s"}
	in, msg := ok.input(true)
	if msg != "" || in.Name != "dr" || in.Endpoint != "storage.yandexcloud.net" {
		t.Fatalf("valid request rejected/unnormalized: %q %+v", msg, in)
	}

	noSecret := ok
	noSecret.SecretAccessKey = ""
	if _, msg := noSecret.input(true); msg == "" {
		t.Fatal("create without secret must fail")
	}
	if _, msg := noSecret.input(false); msg != "" {
		t.Fatalf("update without secret means keep: %q", msg)
	}

	for _, ep := range []string{"https://s3.example", "s3.example/path"} {
		bad := ok
		bad.Endpoint = ep
		if _, msg := bad.input(true); msg == "" {
			t.Fatalf("endpoint %q must be rejected", ep)
		}
	}
}
