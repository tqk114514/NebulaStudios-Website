package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/pprof"
	"time"

	"auth-system/internal/utils"
)

const (
	pprofReadHeaderTimeout = 10 * time.Second
	pprofShutdownTimeout   = 5 * time.Second
)

// newPprofMux 构造 pprof 专用路由。
//
// 显式注册各端点，而不是 `import _ "net/http/pprof"`：
// 后者会把处理器挂到 http.DefaultServeMux，任何误用 DefaultServeMux 的
// http.ListenAndServe 都会把这些诊断接口一起暴露出去。独立 mux 让暴露面可控。
//
// Go 1.27 起 goroutineleak profile 转正，自动包含在 /debug/pprof/ 索引与
// /debug/pprof/goroutineleak 端点中，无需额外注册。
func newPprofMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	return mux
}

// startPprofServer 在独立地址上启动 pprof 服务，返回 server 与实际监听地址。
//
// 与主服务分离的考虑：pprof 端点不经过 Gin 中间件链，因此没有 CSP、限流和鉴权，
// 只能靠监听地址收敛访问面（见 config.validateConfig 的回环校验）。
// 分离也避免诊断流量（profile 采集会 STW 相关采样）影响主服务连接。
func startPprofServer(addr string) (*http.Server, net.Listener, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to bind pprof addr %s: %w", addr, err)
	}

	srv := &http.Server{
		Handler:           newPprofMux(),
		ReadHeaderTimeout: pprofReadHeaderTimeout,
	}

	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			utils.LogError("PPROF", "Serve", err, "pprof server failed")
		}
	}()

	utils.LogInfo("PPROF", "pprof server started", "addr", ln.Addr().String())
	return srv, ln, nil
}

// shutdownPprofServer 关闭 pprof 服务，超时后强制放弃等待在途 profile 采集
func shutdownPprofServer(srv *http.Server) error {
	if srv == nil {
		return nil
	}

	utils.LogInfo("PPROF", "Shutting down pprof server...")
	ctx, cancel := context.WithTimeout(context.Background(), pprofShutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		utils.LogError("PPROF", "Shutdown", err, "pprof server shutdown failed")
		return err
	}

	utils.LogInfo("PPROF", "pprof server stopped")
	return nil
}
