package handler

import "testing"

func TestParseOwnerRepoFromURL(t *testing.T) {
	cases := []struct {
		raw   string
		owner string
		repo  string
		ok    bool
	}{
		{"https://github.com/octocat/Hello-World", "octocat", "Hello-World", true},
		{"https://github.com/octocat/Hello-World.git", "octocat", "Hello-World", true},
		{"https://github.com/octocat/Hello-World/", "octocat", "Hello-World", true},
		{"git@github.com:octocat/Hello-World.git", "octocat", "Hello-World", true},
		{"git@github.com:octocat/Hello-World", "octocat", "Hello-World", true},
		{"ssh://git@github.com/octocat/Hello-World.git", "octocat", "Hello-World", true},
		{"https://gitlab.example.com/group/sub/proj.git", "group", "sub", true}, // 取前两段
		// 失败
		{"", "", "", false},
		{"not a url", "", "", false},
		{"/tmp/helios-e2e-bare", "", "", false},
		{"file:///tmp/foo.git", "", "", false},
		{"https://github.com/onlyowner", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			owner, repo, ok := parseOwnerRepoFromURL(tc.raw)
			if ok != tc.ok || owner != tc.owner || repo != tc.repo {
				t.Errorf("parseOwnerRepoFromURL(%q) = (%q,%q,%v) want (%q,%q,%v)",
					tc.raw, owner, repo, ok, tc.owner, tc.repo, tc.ok)
			}
		})
	}
}
