package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestEmptyControlReport(t *testing.T) {
	var out bytes.Buffer
	if err := run([]string{"-repo", "../..", "-corpus", "arithmetic", "-count", "0", "-warmup", "1"}, &out); err != nil {
		t.Fatal(err)
	}
	var r report
	if err := json.Unmarshal(out.Bytes(), &r); err != nil {
		t.Fatal(err)
	}
	if r.Count != 0 || r.BytesPerParser != nil || r.Warmup != 1 || r.Corpus != "arithmetic" {
		t.Fatalf("unexpected empty control: %+v", r)
	}
	if r.AfterNumGC-r.BeforeNumGC < forcedGCs {
		t.Fatalf("missing post-compilation collections: %+v", r)
	}
	for _, digest := range []string{r.SourceSHA256, r.InputSHA256, r.ResultSHA256} {
		if len(digest) != 64 {
			t.Fatalf("missing workload identity: %q", digest)
		}
	}
	if r.RetainedBytes != int64(r.AfterHeapAlloc)-int64(r.BeforeHeapAlloc) || r.RetainedObjects != int64(r.AfterHeapObjects)-int64(r.BeforeHeapObjects) {
		t.Fatal("report does not describe the live-heap snapshots")
	}
}

func TestInvalidInvocation(t *testing.T) {
	for _, args := range [][]string{{"-count", "-1"}, {"-warmup", "0"}, {"extra"}, {"-corpus", "missing"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			if err := run(args, &bytes.Buffer{}); err == nil {
				t.Fatal("accepted invalid diagnostic invocation")
			}
		})
	}
}

func TestHelp(t *testing.T) {
	var out bytes.Buffer
	if err := run([]string{"-h"}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "-count") || !strings.Contains(out.String(), "-corpus") {
		t.Fatal("help omits measurement controls")
	}
}
