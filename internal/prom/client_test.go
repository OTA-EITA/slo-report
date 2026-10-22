package prom

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func server(t *testing.T, body string, check func(*http.Request)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if check != nil {
			check(r)
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestScalarVector(t *testing.T) {
	at := time.Unix(1700000000, 0)
	srv := server(t, `{"status":"success","data":{"resultType":"vector","result":[{"metric":{},"value":[1700000000,"0.0123"]}]}}`, func(r *http.Request) {
		if r.URL.Path != "/api/v1/query" || r.URL.Query().Get("query") != "up" || r.URL.Query().Get("time") != "1700000000" {
			t.Errorf("unexpected request %s", r.URL)
		}
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("missing bearer token")
		}
	})
	v, err := Client{BaseURL: srv.URL + "/", BearerToken: "secret"}.Scalar(context.Background(), "up", at)
	if err != nil || v != 0.0123 {
		t.Fatalf("got %v, %v", v, err)
	}
}

func TestScalarResultType(t *testing.T) {
	srv := server(t, `{"status":"success","data":{"resultType":"scalar","result":[1700000000,"2"]}}`, nil)
	if v, err := (Client{BaseURL: srv.URL}).Scalar(context.Background(), "2", time.Now()); err != nil || v != 2 {
		t.Fatalf("got %v, %v", v, err)
	}
}

func TestScalarNoData(t *testing.T) {
	for _, body := range []string{
		`{"status":"success","data":{"resultType":"vector","result":[]}}`,
		`{"status":"success","data":{"resultType":"vector","result":[{"metric":{},"value":[1,"NaN"]}]}}`,
	} {
		srv := server(t, body, nil)
		if _, err := (Client{BaseURL: srv.URL}).Scalar(context.Background(), "x", time.Now()); !errors.Is(err, ErrNoData) {
			t.Errorf("%s: got %v, want ErrNoData", body, err)
		}
	}
}

func TestScalarErrors(t *testing.T) {
	cases := map[string]string{
		`{"status":"error","errorType":"bad_data","error":"parse error"}`:                                    "parse error",
		`{"status":"success","data":{"resultType":"vector","result":[{"value":[1,"1"]},{"value":[1,"2"]}]}}`: "aggregate it with sum()",
		`{"status":"success","data":{"resultType":"matrix","result":[]}}`:                                    "unsupported result type",
		`<html>bad gateway</html>`: "returned HTTP",
	}
	for body, want := range cases {
		srv := server(t, body, nil)
		_, err := (Client{BaseURL: srv.URL}).Scalar(context.Background(), "x", time.Now())
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: got %v, want %q", body, err, want)
		}
	}
}
