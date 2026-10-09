package storage

import "testing"

func TestLabels(t *testing.T) {
	b := JobBucket{ID: BuiltInID, Name: "helm", Endpoint: "storage.yandexcloud.net/", Bucket: "dumps"}
	if got, want := b.String(), "s3://dumps (storage.yandexcloud.net)"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
	if got, want := b.Object("db/x.dump"), "s3://dumps/db/x.dump (storage.yandexcloud.net)"; got != want {
		t.Errorf("Object() = %q, want %q", got, want)
	}
}
