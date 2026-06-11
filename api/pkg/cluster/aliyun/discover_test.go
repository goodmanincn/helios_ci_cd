package aliyun

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
		AccessKeyID: "ak", AccessKeySecret: "sk", Region: "cn-hangzhou",
	}, "c-abc")
	if err != nil {
		t.Fatal(err)
	}
	if len(b) == 0 {
		t.Fatal("empty json")
	}
}
