package services

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"os/exec"
)

// swapEmbeddedBinary 用一段小数据替换嵌入的 Zig 二进制，避免测试反复写入 6MB 文件
func swapEmbeddedBinary(t *testing.T, data []byte) {
	t.Helper()
	original := imgProcessorBin
	imgProcessorBin = data
	t.Cleanup(func() { imgProcessorBin = original })
}

// newTestProcessor 构造注入了接缝的处理器；startProcess 默认立即失败，避免真实 exec
func newTestProcessor(t *testing.T) *ImgProcessor {
	t.Helper()

	p := &ImgProcessor{sem: make(chan struct{}, MaxConcurrent)}
	// 预置 restarting=true 以抑制后台重启：ToWebP 的 dial 失败路径会顺带调用
	// checkAndRestart → tryRestart，抛出读取包级全局 imgProcessorBin 的后台 goroutine
	// （imgprocessor.go:315-317）。用例只断言返回值、不会 join 它，
	// 于是与下一个测试的 swapEmbeddedBinary 形成数据竞态（-race 下必现）。
	// 置位后 tryRestart 直接短路返回；确实要测重启行为的用例显式清掉该标志。
	p.restarting = true
	p.startProcess = func(string, string) (*exec.Cmd, error) {
		return nil, errors.New("test stub: process start disabled")
	}
	t.Cleanup(func() {
		p.mu.Lock()
		dir := p.tempDir
		p.mu.Unlock()
		if dir != "" {
			_ = os.RemoveAll(dir)
		}
	})
	return p
}

// pipeDialer 用内存管道模拟到处理进程的连接
func pipeDialer(t *testing.T, server func(conn net.Conn)) func(context.Context, string, string) (net.Conn, error) {
	t.Helper()
	return func(context.Context, string, string) (net.Conn, error) {
		client, srv := net.Pipe()
		go func() {
			defer srv.Close()
			server(srv)
		}()
		return client, nil
	}
}

// fakeZig 模拟 Zig 端的响应协议：读 4 字节长度 + 负载，回写 status + 4 字节长度 + 数据
func fakeZig(status byte, response []byte) func(net.Conn) {
	return func(conn net.Conn) {
		lenBuf := make([]byte, 4)
		if _, err := io.ReadFull(conn, lenBuf); err != nil {
			return
		}
		payload := make([]byte, binary.BigEndian.Uint32(lenBuf))
		if _, err := io.ReadFull(conn, payload); err != nil {
			return
		}

		out := make([]byte, 5)
		out[0] = status
		binary.BigEndian.PutUint32(out[1:], uint32(len(response)))
		_, _ = conn.Write(out)
		if len(response) > 0 {
			_, _ = conn.Write(response)
		}
	}
}

// fakeZigAcceptRequest 完整读掉 4 字节长度 + 负载后直接返回，不回写 status 字节。
// 与 fakeZig 的区别只在于「收不到响应」，用于把失败点精确留在 read status 这一步。
func fakeZigAcceptRequest() func(net.Conn) {
	return func(conn net.Conn) {
		lenBuf := make([]byte, 4)
		if _, err := io.ReadFull(conn, lenBuf); err != nil {
			return
		}
		payload := make([]byte, binary.BigEndian.Uint32(lenBuf))
		if _, err := io.ReadFull(conn, payload); err != nil {
			return
		}
	}
}

func TestStartProcessorSuccess(t *testing.T) {
	swapEmbeddedBinary(t, []byte("fake-zig-binary"))
	p := newTestProcessor(t)

	// 注入的"进程"顺手创建 socket 文件，模拟就绪
	var gotBinaryPath, gotSocketPath string
	p.startProcess = func(binaryPath, socketPath string) (*exec.Cmd, error) {
		gotBinaryPath, gotSocketPath = binaryPath, socketPath
		if err := os.WriteFile(socketPath, nil, 0o600); err != nil {
			return nil, err
		}
		return nil, nil
	}

	p.startProcessor()

	if !p.available {
		t.Fatalf("processor should be available after socket appears (socketPath=%q)", p.socketPath)
	}
	// socket 必须落在私有临时目录内
	if filepath.Dir(gotSocketPath) != p.tempDir {
		t.Errorf("socket dir = %q, want private temp dir %q", filepath.Dir(gotSocketPath), p.tempDir)
	}
	// 二进制内容应与嵌入数据一致（写入后做过哈希校验）
	written, err := os.ReadFile(gotBinaryPath)
	if err != nil {
		t.Fatalf("read extracted binary: %v", err)
	}
	if string(written) != "fake-zig-binary" {
		t.Errorf("binary content = %q, want embedded data", written)
	}
}

func TestStartProcessorWithoutEmbeddedBinary(t *testing.T) {
	swapEmbeddedBinary(t, nil)
	p := newTestProcessor(t)

	p.startProcessor()

	if p.available {
		t.Error("processor should not be available without embedded binary")
	}
}

func TestStartProcessorFailureCleansUp(t *testing.T) {
	swapEmbeddedBinary(t, []byte("fake-zig-binary"))
	p := newTestProcessor(t)

	p.startProcess = func(string, string) (*exec.Cmd, error) {
		return nil, errors.New("exec failed")
	}

	p.startProcessor()

	if p.available {
		t.Error("processor should not be available when start fails")
	}
	if p.tempDir == "" {
		t.Fatal("tempDir should have been created before start")
	}
	if _, err := os.Stat(p.tempDir); !os.IsNotExist(err) {
		t.Errorf("temp dir %q should be removed after start failure", p.tempDir)
	}
}

func TestToWebPRejectsBadInput(t *testing.T) {
	p := newTestProcessor(t)

	if _, err := p.ToWebP(nil); err == nil {
		t.Error("empty image data should be rejected")
	}
	if _, err := p.ToWebP(make([]byte, MaxImageSize+1)); !errors.Is(err, ErrImageTooLarge) {
		t.Errorf("oversized image error = %v, want ErrImageTooLarge", err)
	}
}

func TestToWebPSuccess(t *testing.T) {
	p := newTestProcessor(t)
	p.socketPath = "test-socket"
	p.dialContext = pipeDialer(t, fakeZig(0, []byte("webp-bytes")))

	out, err := p.ToWebP([]byte("png-bytes"))
	if err != nil {
		t.Fatalf("ToWebP: %v", err)
	}
	if string(out) != "webp-bytes" {
		t.Errorf("output = %q, want webp-bytes", out)
	}
	if !p.available {
		t.Error("successful round trip should mark processor available")
	}
}

// 处理失败时返回 Zig 端给出的原因
func TestToWebPProcessFailure(t *testing.T) {
	p := newTestProcessor(t)
	p.socketPath = "test-socket"
	p.dialContext = pipeDialer(t, fakeZig(1, []byte("unsupported format")))

	_, err := p.ToWebP([]byte("not-an-image"))
	if !errors.Is(err, ErrProcessFailed) {
		t.Fatalf("error = %v, want ErrProcessFailed", err)
	}
	if err.Error() == "" || !strings.Contains(err.Error(), "unsupported format") {
		t.Errorf("error = %v, want upstream reason included", err)
	}
}

// 响应长度超过上限时拒绝读取，避免被异常进程撑爆内存
func TestToWebPRejectsOversizedResponse(t *testing.T) {
	p := newTestProcessor(t)
	p.socketPath = "test-socket"
	p.dialContext = pipeDialer(t, func(conn net.Conn) {
		lenBuf := make([]byte, 4)
		if _, err := io.ReadFull(conn, lenBuf); err != nil {
			return
		}
		payload := make([]byte, binary.BigEndian.Uint32(lenBuf))
		if _, err := io.ReadFull(conn, payload); err != nil {
			return
		}

		out := make([]byte, 5)
		out[0] = 0
		binary.BigEndian.PutUint32(out[1:], uint32(MaxImageSize+1))
		_, _ = conn.Write(out)
	})

	_, err := p.ToWebP([]byte("png"))
	if err == nil || !strings.Contains(err.Error(), "response too large") {
		t.Errorf("error = %v, want response too large", err)
	}
}

func TestToWebPDialFailure(t *testing.T) {
	swapEmbeddedBinary(t, []byte("fake-zig-binary"))
	p := newTestProcessor(t)
	p.dialContext = func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("connection refused")
	}

	_, err := p.ToWebP([]byte("png"))
	if !errors.Is(err, ErrProcessorNotAvailable) {
		t.Errorf("error = %v, want ErrProcessorNotAvailable", err)
	}
	if p.available {
		t.Error("processor should be marked unavailable after dial failure")
	}
}

// 对端提前关闭：读状态码失败
// peer 必须先把请求完整读走，否则 net.Pipe 无缓冲，客户端第一个 Write 就会
// 因对端已关闭而失败（错误落在 write length failed，走不到 read status）
func TestToWebPPeerClosed(t *testing.T) {
	p := newTestProcessor(t)
	p.socketPath = "test-socket"
	p.dialContext = pipeDialer(t, fakeZigAcceptRequest())

	_, err := p.ToWebP([]byte("png"))
	if err == nil || !strings.Contains(err.Error(), "read status failed") {
		t.Errorf("error = %v, want read status failed", err)
	}
}

func TestIsAvailable(t *testing.T) {
	p := newTestProcessor(t)

	if p.IsAvailable() {
		t.Error("fresh processor should be unavailable")
	}
	p.available = true
	if !p.IsAvailable() {
		t.Error("IsAvailable should reflect the flag")
	}
}

// 未启动过进程时 Shutdown 应安全返回
func TestShutdownWithoutProcess(t *testing.T) {
	p := newTestProcessor(t)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	p.Shutdown(ctx)
}

// 进程为 nil 时 checkAndRestart 走 tryRestart；注入的 start 立即失败，不应 panic
func TestCheckAndRestartWithoutProcess(t *testing.T) {
	swapEmbeddedBinary(t, []byte("fake-zig-binary"))
	p := newTestProcessor(t)

	// 本用例测的就是 tryRestart 确实跑起来，解除 fixture 的抑制
	p.mu.Lock()
	p.restarting = false
	p.mu.Unlock()

	p.checkAndRestart()

	// 等待后台重启流程结束（注入的 start 立即失败）
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		p.mu.Lock()
		restarting := p.restarting
		p.mu.Unlock()
		if !restarting {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	p.mu.Lock()
	restarting := p.restarting
	p.mu.Unlock()
	if restarting {
		t.Error("restart flag should be cleared after the attempt")
	}
}

// socket 文件仍存在时不应触发重启
func TestCheckAndRestartSkipsWhenSocketExists(t *testing.T) {
	swapEmbeddedBinary(t, []byte("fake-zig-binary"))
	p := newTestProcessor(t)

	dir := t.TempDir()
	sock := filepath.Join(dir, "img.sock")
	if err := os.WriteFile(sock, nil, 0o600); err != nil {
		t.Fatalf("write socket placeholder: %v", err)
	}
	p.socketPath = sock

	started := make(chan struct{}, 1)
	p.startProcess = func(string, string) (*exec.Cmd, error) {
		select {
		case started <- struct{}{}:
		default:
		}
		return nil, errors.New("should not be called")
	}

	// cmd 必须带一个非 nil 的 Process：checkAndRestart 的守卫是
	// 「cmd == nil || cmd.Process == nil → 重启」，&exec.Cmd{} 会被判成没有活进程。
	// 这里用一个立刻退出的子进程而不是测试自身的 PID，这样即使守卫被改错，
	// tryRestart 里的 Process.Kill() 杀掉的也是这个弃用子进程，不会把测试进程打死
	live := exec.Command(os.Args[0], "-test.list=^$")
	if err := live.Start(); err != nil {
		t.Fatalf("启动占位子进程失败: %v", err)
	}
	t.Cleanup(func() {
		_ = live.Process.Kill()
		_ = live.Wait()
	})

	// 解除 fixture 抑制，否则 tryRestart 会因 restarting 预置位而短路，
	// 即使守卫被改错也观测不到重启，本用例就失去意义
	p.mu.Lock()
	p.restarting = false
	p.mu.Unlock()

	// cmd 非 nil 且 socket 存在 → 不重启
	p.cmd = live
	p.checkAndRestart()

	select {
	case <-started:
		t.Error("restart should not be attempted while the socket exists")
	case <-time.After(200 * time.Millisecond):
	}
}
