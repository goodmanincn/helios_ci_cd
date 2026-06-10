package tasks

import (
	"encoding/json"
	"testing"
)

func TestGitCheckoutPayload_Validate(t *testing.T) {
	cases := []struct {
		name string
		p    GitCheckoutPayload
		ok   bool
	}{
		{"happy", GitCheckoutPayload{RunID: 1, ProjectID: 2, RepoURL: "https://github.com/a/b.git", Branch: "main"}, true},
		{"missing run_id", GitCheckoutPayload{ProjectID: 2, RepoURL: "x", Branch: "y"}, false},
		{"missing project_id", GitCheckoutPayload{RunID: 1, RepoURL: "x", Branch: "y"}, false},
		{"missing repo_url", GitCheckoutPayload{RunID: 1, ProjectID: 2, Branch: "y"}, false},
		{"missing branch", GitCheckoutPayload{RunID: 1, ProjectID: 2, RepoURL: "x"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.p.Validate()
			if (err == nil) != tc.ok {
				t.Fatalf("expect ok=%v got err=%v", tc.ok, err)
			}
		})
	}
}

func TestGitCheckout_MarshalRoundtrip(t *testing.T) {
	src := &GitCheckoutPayload{
		RunID: 42, ProjectID: 7, RepoURL: "git@github.com:a/b.git",
		Branch: "feature/x", CommitSHA: "deadbeef",
	}
	b, err := src.Marshal()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got, err := UnmarshalGitCheckout(b)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if *got != *src {
		t.Fatalf("roundtrip mismatch: %+v vs %+v", got, src)
	}
}

func TestUnmarshalGitCheckout_BadJSON(t *testing.T) {
	if _, err := UnmarshalGitCheckout([]byte("not json")); err == nil {
		t.Fatal("expect error on bad json")
	}
	bad, _ := json.Marshal(map[string]any{"run_id": 0})
	if _, err := UnmarshalGitCheckout(bad); err == nil {
		t.Fatal("expect validation error")
	}
}

// ===== ApprovalTimeoutPayload (T2.6.3) =====

func TestApprovalTimeoutPayload_Roundtrip(t *testing.T) {
	src := &ApprovalTimeoutPayload{RequestID: 99}
	b, err := src.Marshal()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got, err := UnmarshalApprovalTimeout(b)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if *got != *src {
		t.Fatalf("roundtrip mismatch: %+v vs %+v", got, src)
	}
}

func TestApprovalTimeoutPayload_Validate(t *testing.T) {
	if (&ApprovalTimeoutPayload{RequestID: 1}).Validate() != nil {
		t.Fatal("expect ok")
	}
	if (&ApprovalTimeoutPayload{}).Validate() == nil {
		t.Fatal("expect err on zero request_id")
	}
}
