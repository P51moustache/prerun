package main

import (
	"strings"
	"testing"
)

func parse(t *testing.T, src string) (*Pipeline, []string, error) {
	t.Helper()
	return parsePipelineBytes("test.yml", []byte(src))
}

func mustParse(t *testing.T, src string) (*Pipeline, []string) {
	t.Helper()
	p, warns, err := parse(t, src)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	return p, warns
}

func wantErr(t *testing.T, src, substr string) {
	t.Helper()
	_, _, err := parse(t, src)
	if err == nil {
		t.Fatalf("expected error containing %q, got nil", substr)
	}
	if !strings.Contains(err.Error(), substr) {
		t.Fatalf("expected error containing %q, got: %v", substr, err)
	}
}

func TestParseFixture(t *testing.T) {
	p, warns, err := parsePipeline("testdata/pipeline.yml")
	if err != nil {
		t.Fatal(err)
	}
	if len(warns) != 0 {
		t.Fatalf("fixture should parse without warnings, got %v", warns)
	}
	got := make([]string, len(p.Jobs))
	for i, j := range p.Jobs {
		got[i] = j.Name
	}
	want := []string{"build:app", "test:app", "package:app"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("job order = %v, want %v", got, want)
		}
	}
}

func TestScriptStringForm(t *testing.T) {
	p, _ := mustParse(t, "j:\n  stage: test\n  script: echo one\n")
	if len(p.Jobs[0].Script) != 1 || p.Jobs[0].Script[0] != "echo one" {
		t.Fatalf("script = %v", p.Jobs[0].Script)
	}
}

func TestNeedsObjectForm(t *testing.T) {
	p, _ := mustParse(t, `
stages: [build, test]
a:
  stage: build
  script: echo a
b:
  stage: test
  needs: [{job: a}]
  script: echo b
`)
	if p.Jobs[1].Needs[0].Job != "a" {
		t.Fatalf("needs = %v", p.Jobs[1].Needs)
	}
}

func TestUnknownStageRejected(t *testing.T) {
	wantErr(t, "j:\n  stage: nope\n  script: echo hi\n", `stage "nope"`)
}

func TestDuplicateJobRejected(t *testing.T) {
	wantErr(t, "j:\n  script: echo one\nj:\n  script: echo two\n", "defined twice")
}

func TestNeedsUnknownJobRejected(t *testing.T) {
	wantErr(t, "j:\n  script: echo hi\n  needs: [ghost]\n", "not a defined job")
}

func TestNeedsTraversalRejected(t *testing.T) {
	// Security: a needs entry must never become a host path component.
	wantErr(t, "j:\n  script: echo hi\n  needs: ['..']\n", "not a defined job")
}

func TestNeedsLaterStageRejected(t *testing.T) {
	wantErr(t, `
stages: [build, test]
early:
  stage: build
  needs: [late]
  script: echo hi
late:
  stage: test
  script: echo hi
`, "earlier stage")
}

func TestGlobArtifactRejected(t *testing.T) {
	wantErr(t, "j:\n  script: echo hi\n  artifacts:\n    paths: ['dist/*.jar']\n", "glob")
}

func TestJobLevelUnsupportedFieldWarns(t *testing.T) {
	_, warns := mustParse(t, "j:\n  script: echo hi\n  rules:\n    - if: $CI\n")
	found := false
	for _, w := range warns {
		if strings.Contains(w, `"rules"`) {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a warning about rules, got %v", warns)
	}
}

func TestHiddenJobWarns(t *testing.T) {
	_, warns := mustParse(t, ".tmpl:\n  script: echo hi\nj:\n  script: echo hi\n")
	found := false
	for _, w := range warns {
		if strings.Contains(w, ".tmpl") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a warning about .tmpl, got %v", warns)
	}
}

func TestArtifactSources(t *testing.T) {
	p, _ := mustParse(t, `
stages: [build, test, package]
a:
  stage: build
  script: echo a
  artifacts: {paths: [out/]}
b:
  stage: build
  script: echo b
  artifacts: {paths: [out2/]}
c:
  stage: test
  script: echo c
d:
  stage: package
  needs: [a]
  script: echo d
e:
  stage: package
  dependencies: [b]
  script: echo e
`)
	if got := p.artifactSources(p.job("c")); len(got) != 2 {
		t.Fatalf("c should default to all earlier artifact producers, got %v", got)
	}
	if got := p.artifactSources(p.job("d")); len(got) != 1 || got[0] != "a" {
		t.Fatalf("needs should win, got %v", got)
	}
	if got := p.artifactSources(p.job("e")); len(got) != 1 || got[0] != "b" {
		t.Fatalf("dependencies should be honored, got %v", got)
	}
}

func TestParseBreakpoint(t *testing.T) {
	cases := []struct {
		spec string
		job  string
		step int
	}{
		{"build", "build", 1},
		{"build:3", "build", 3},
		{"test:app", "test:app", 1},   // colon in job name, no step
		{"test:app:2", "test:app", 2}, // colon in job name plus step
	}
	for _, c := range cases {
		job, step := parseBreakpoint(c.spec)
		if job != c.job || step != c.step {
			t.Errorf("parseBreakpoint(%q) = (%q,%d), want (%q,%d)", c.spec, job, step, c.job, c.step)
		}
	}
}

func TestSanitize(t *testing.T) {
	cases := map[string]string{
		"build:app": "build__app",
		"a/b":       "a__b",
		"..":        "job_..",
		".":         "job_.",
	}
	for in, want := range cases {
		if got := sanitize(in); got != want {
			t.Errorf("sanitize(%q) = %q, want %q", in, got, want)
		}
	}
}
