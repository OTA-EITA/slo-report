package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const example = "../../examples/slos.yaml"

func fixedNow() time.Time { return time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC) }

func TestValidate(t *testing.T) {
	var out bytes.Buffer
	if code := run([]string{"validate", example}, &out, io.Discard, fixedNow); code != exitOK {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(out.String(), "api-availability") || !strings.Contains(out.String(), "budget=0.1%") {
		t.Errorf("output:\n%s", out.String())
	}
	bad := filepath.Join(t.TempDir(), "bad.yaml")
	if err := os.WriteFile(bad, []byte("slos: [{name: x}]"), 0o600); err != nil {
		t.Fatal(err)
	}
	var errOut bytes.Buffer
	if code := run([]string{"validate", bad}, io.Discard, &errOut, fixedNow); code != exitFail || !strings.Contains(errOut.String(), "objective") {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
}

func TestGenerateMatchesGolden(t *testing.T) {
	var out bytes.Buffer
	if code := run([]string{"generate", example}, &out, io.Discard, fixedNow); code != exitOK {
		t.Fatalf("exit %d", code)
	}
	golden, err := os.ReadFile("../../examples/rules.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if out.String() != string(golden) {
		t.Fatal("generate output differs from examples/rules.yaml")
	}
	path := filepath.Join(t.TempDir(), "rules.yaml")
	if code := run([]string{"generate", "-o", path, example}, io.Discard, io.Discard, fixedNow); code != exitOK {
		t.Fatalf("exit %d", code)
	}
}

// promStub answers every query with the same error ratio.
func promStub(t *testing.T, ratio string) string {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[{"metric":{},"value":[1,"` + ratio + `"]}]}}`))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestReportExitCodes(t *testing.T) {
	healthy := promStub(t, "0.0001")
	var out bytes.Buffer
	if code := run([]string{"report", "--prometheus", healthy, "--output", "markdown", example}, &out, io.Discard, fixedNow); code != exitOK {
		t.Fatalf("healthy: exit %d\n%s", code, out.String())
	}
	if !strings.Contains(out.String(), "🟢 HEALTHY") {
		t.Errorf("markdown:\n%s", out.String())
	}

	exhausted := promStub(t, "0.5")
	if code := run([]string{"report", "--prometheus", exhausted, example}, io.Discard, io.Discard, fixedNow); code != exitFail {
		t.Fatalf("exhausted budgets must exit 1, got %d", code)
	}
	if code := run([]string{"report", "--prometheus", exhausted, "--fail-on", "never", example}, io.Discard, io.Discard, fixedNow); code != exitOK {
		t.Fatalf("--fail-on never must exit 0, got %d", code)
	}
}

func TestUsageErrors(t *testing.T) {
	cases := [][]string{
		{},
		{"bogus"},
		{"report", example},
		{"report", "--prometheus", "http://x", "--output", "yaml", example},
		{"report", "--prometheus", "http://x", "--fail-on", "sometimes", example},
	}
	for _, args := range cases {
		t.Setenv("PROMETHEUS_URL", "")
		if code := run(args, io.Discard, io.Discard, fixedNow); code != exitUsage {
			t.Errorf("run(%v) = %d, want %d", args, code, exitUsage)
		}
	}
}
