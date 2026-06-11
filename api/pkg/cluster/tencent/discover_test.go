package tencent

import (
	"context"
	"testing"
)

func TestListClusters_Validation(t *testing.T) {
	_, err := ListClusters(context.Background(), CloudCredentials{})
	if err == nil {
		t.Fatal("expected error for empty creds")
	}
}

func TestCredentialsJSON(t *testing.T) {
	b, err := CredentialsJSON(CloudCredentials{
		SecretID: "id", SecretKey: "key", Region: "ap-shanghai",
	}, "cls-abc")
	if err != nil {
		t.Fatal(err)
	}
	if len(b) == 0 {
		t.Fatal("empty json")
	}
}
