package plugin

import (
	"strings"
	"testing"
)

func TestParseActionYAML_Minimal(t *testing.T) {
	src := []byte(`name: echo
description: prints input
runs:
  using: container
  image: alpine:3
`)
	a, errs := ParseActionYAML(src)
	if len(errs) > 0 {
		t.Fatalf("unexpected errs: %v", errs)
	}
	if a.Name != "echo" {
		t.Errorf("name=%q", a.Name)
	}
	if a.Runs.Using != "container" || a.Runs.Image != "alpine:3" {
		t.Errorf("runs=%+v", a.Runs)
	}
}

func TestParseActionYAML_MissingName(t *testing.T) {
	_, errs := ParseActionYAML([]byte(`runs:
  using: container
  image: x
`))
	if len(errs) == 0 {
		t.Fatal("expected errors")
	}
	if !containsErr(errs, "name: required") {
		t.Errorf("want name required, got %v", errs)
	}
}

func TestParseActionYAML_ContainerMissingImage(t *testing.T) {
	_, errs := ParseActionYAML([]byte(`name: x
runs:
  using: container
`))
	if !containsErr(errs, "runs.image: required") {
		t.Errorf("want image required, got %v", errs)
	}
}

func TestParseActionYAML_Composite(t *testing.T) {
	src := []byte(`name: pipeline-suite
runs:
  using: composite
  steps:
    - run: echo hello
    - uses: helios/echo@v1
      with: {message: world}
`)
	a, errs := ParseActionYAML(src)
	if len(errs) > 0 {
		t.Fatalf("unexpected errs: %v", errs)
	}
	if len(a.Runs.Steps) != 2 {
		t.Errorf("steps=%d", len(a.Runs.Steps))
	}
}

func TestParseActionYAML_JavaScriptUnsupported(t *testing.T) {
	_, errs := ParseActionYAML([]byte(`name: x
runs:
  using: javascript
  main: index.js
`))
	if !containsErr(errs, "javascript not yet supported") {
		t.Errorf("want js unsupported, got %v", errs)
	}
}

func TestParseActionYAML_UnknownPermission(t *testing.T) {
	_, errs := ParseActionYAML([]byte(`name: x
runs:
  using: container
  image: alpine
needs_permissions: [network, kernel]
`))
	if !containsErr(errs, "needs_permissions: unknown") {
		t.Errorf("want unknown perm, got %v", errs)
	}
}

func TestParseActionYAML_FullInputsOutputs(t *testing.T) {
	src := []byte(`name: full
description: kitchen sink
author: helios
inputs:
  message:
    description: text to echo
    required: true
    type: string
  count:
    type: number
    default: 1
outputs:
  echoed:
    description: what we echoed
runs:
  using: container
  image: alpine:3
  pull_policy: IfNotPresent
  env:
    FOO: bar
needs_secrets: [API_TOKEN]
needs_permissions: [network]
`)
	a, errs := ParseActionYAML(src)
	if len(errs) > 0 {
		t.Fatalf("unexpected errs: %v", errs)
	}
	if a.Inputs["message"].Required != true {
		t.Errorf("message required not parsed")
	}
	if a.Inputs["count"].Default != 1 {
		t.Errorf("count default=%v", a.Inputs["count"].Default)
	}
	if a.Outputs["echoed"].Description == "" {
		t.Errorf("outputs not parsed")
	}
	if len(a.NeedsSecrets) != 1 || a.NeedsSecrets[0] != "API_TOKEN" {
		t.Errorf("secrets=%v", a.NeedsSecrets)
	}
}

func TestParseRef(t *testing.T) {
	cases := []struct {
		in       string
		ok       bool
		ns, name string
		ver      string
		local    bool
	}{
		{"helios/echo@v1", true, "helios", "echo", "v1", false},
		{"acme/foo@1.2.3", true, "acme", "foo", "1.2.3", false},
		{"acme/foo@latest", true, "acme", "foo", "latest", false},
		{"./local", true, "", "", "", true},
		{"github.com/foo/bar@v1", false, "", "", "", false},
		{"foo@v1", false, "", "", "", false},
		{"foo/bar", false, "", "", "", false},
		{"foo/bar@", false, "", "", "", false},
		{"", false, "", "", "", false},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			r, err := ParseRef(c.in)
			if (err == nil) != c.ok {
				t.Fatalf("err=%v, want ok=%v", err, c.ok)
			}
			if !c.ok {
				return
			}
			if c.local {
				if !r.Local {
					t.Errorf("expected local")
				}
				return
			}
			if r.Namespace != c.ns || r.Name != c.name || r.Version != c.ver {
				t.Errorf("got %+v", r)
			}
			if r.Slug() != c.ns+"/"+c.name {
				t.Errorf("slug=%s", r.Slug())
			}
		})
	}
}

func containsErr(errs []error, sub string) bool {
	for _, e := range errs {
		if strings.Contains(e.Error(), sub) {
			return true
		}
	}
	return false
}
