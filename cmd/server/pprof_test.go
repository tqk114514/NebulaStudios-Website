package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"runtime/pprof"
	"testing"
)

// mux 与 Go 版本契约：Go 1.27 起 goroutineleak 是内置 profile，
// 无需手动注册即可通过 /debug/pprof/<name> 取到。此处断言该契约，
// 若未来 profile 被重命名或默认关闭，测试会先于线上失效。
func TestNewPprofMux_ServesGoroutineLeak(t *testing.T) {
	if pprof.Lookup("goroutineleak") == nil {
		t.Fatal("goroutineleak profile is not registered by the runtime")
	}

	srv := httptest.NewServer(newPprofMux())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/debug/pprof/goroutineleak?debug=0")
	if err != nil {
		t.Fatalf("request goroutineleak: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if len(body) == 0 {
		t.Fatal("goroutineleak profile returned empty body")
	}
}

func TestNewPprofMux_IndexAndUnknownProfile(t *testing.T) {
	srv := httptest.NewServer(newPprofMux())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/debug/pprof/")
	if err != nil {
		t.Fatalf("request index: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("index status = %d, want 200", resp.StatusCode)
	}

	resp, err = http.Get(srv.URL + "/debug/pprof/definitely-not-a-profile")
	if err != nil {
		t.Fatalf("request unknown profile: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown profile status = %d, want 404", resp.StatusCode)
	}
}

func TestStartPprofServer_Lifecycle(t *testing.T) {
	srv, ln, err := startPprofServer("127.0.0.1:0")
	if err != nil {
		t.Fatalf("startPprofServer: %v", err)
	}
	addr := ln.Addr().String()

	resp, err := http.Get("http://" + addr + "/debug/pprof/")
	if err != nil {
		t.Fatalf("request running server: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	if err := shutdownPprofServer(srv); err != nil {
		t.Fatalf("shutdownPprofServer: %v", err)
	}

	if _, err := http.Get("http://" + addr + "/debug/pprof/"); err == nil {
		t.Fatal("expected request to fail after shutdown")
	}
}

func TestShutdownPprofServer_NilSafe(t *testing.T) {
	if err := shutdownPprofServer(nil); err != nil {
		t.Fatalf("shutdownPprofServer(nil) = %v, want nil", err)
	}
}
