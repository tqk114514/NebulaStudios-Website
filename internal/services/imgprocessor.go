/**
 * internal/services/imgprocessor.go
 * 图片处理服务客户端
 *
 * 通过 Unix Socket 与 Rust img-processor 通信
 * 将图片转换为 WebP 格式
 */

package services

import (
	_ "embed"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"sync"
	"time"

	"auth-system/internal/utils"
)

const (
	// SocketPath Unix Socket 路径
	SocketPath = "/tmp/img-processor.sock"
	// BinaryPath 二进制文件路径
	BinaryPath = "/tmp/img-processor"
	// ConnectTimeout 连接超时
	ConnectTimeout = 5 * time.Second
	// ReadWriteTimeout 读写超时
	ReadWriteTimeout = 30 * time.Second
	// MaxImageSize 最大图片大小
	MaxImageSize = 10 * 1024 * 1024 // 10MB
	// MaxConcurrent 最大并发数（避免 CPU 过载）
	MaxConcurrent = 2
)

var (
	// ErrProcessorNotAvailable 处理器不可用
	ErrProcessorNotAvailable = errors.New("image processor not available")
	// ErrImageTooLarge 图片太大
	ErrImageTooLarge = errors.New("image too large")
	// ErrProcessFailed 处理失败
	ErrProcessFailed = errors.New("image process failed")
)

// 嵌入 Rust 二进制（编译时由 GitHub Actions 放置）
// 如果文件不存在，embed 会失败，所以用 embed.FS 更安全
//
//go:embed img-processor-bin
var imgProcessorBin []byte

// ImgProcessor 图片处理服务
type ImgProcessor struct {
	mu         sync.Mutex
	available  bool
	sem        chan struct{} // 并发限制信号量
	cmd        *exec.Cmd     // 子进程
	restarting bool          // 是否正在重启
}

// NewImgProcessor 创建图片处理服务
func NewImgProcessor() *ImgProcessor {
	p := &ImgProcessor{
		sem: make(chan struct{}, MaxConcurrent),
	}
	p.startProcessor()
	return p
}

// startProcessor 启动 Rust 处理器
func (p *ImgProcessor) startProcessor() {
	// 检查嵌入的二进制是否存在
	if len(imgProcessorBin) == 0 {
		utils.LogPrintf("[IMG] WARN: Embedded binary not found, using fallback")
		p.available = false
		return
	}

	// 释放二进制文件
	if err := os.WriteFile(BinaryPath, imgProcessorBin, 0755); err != nil {
		utils.LogPrintf("[IMG] ERROR: Failed to write binary: %v", err)
		p.available = false
		return
	}

	// 删除旧的 socket
	os.Remove(SocketPath)

	// 启动进程
	p.cmd = exec.Command(BinaryPath)
	if err := p.cmd.Start(); err != nil {
		utils.LogPrintf("[IMG] ERROR: Failed to start processor: %v", err)
		p.available = false
		return
	}

	// 等待 socket 就绪
	for i := 0; i < 50; i++ { // 最多等 5 秒
		time.Sleep(100 * time.Millisecond)
		if _, err := os.Stat(SocketPath); err == nil {
			p.available = true
			utils.LogPrintf("[IMG] Image processor started (PID: %d)", p.cmd.Process.Pid)
			return
		}
	}

	utils.LogPrintf("[IMG] WARN: Processor started but socket not ready")
	p.available = false
}

// Shutdown 关闭处理器
func (p *ImgProcessor) Shutdown() {
	if p.cmd != nil && p.cmd.Process != nil {
		p.cmd.Process.Kill()
		p.cmd.Wait()
		utils.LogPrintf("[IMG] Image processor stopped")
	}
	os.Remove(SocketPath)
	os.Remove(BinaryPath)
}

// IsAvailable 检查服务是否可用
func (p *ImgProcessor) IsAvailable() bool {
	return p.available
}

// tryRestart 尝试重启处理器（非阻塞，后台执行）
func (p *ImgProcessor) tryRestart() {
	p.mu.Lock()
	if p.restarting {
		p.mu.Unlock()
		return
	}
	p.restarting = true
	p.mu.Unlock()

	go func() {
		defer func() {
			p.mu.Lock()
			p.restarting = false
			p.mu.Unlock()
		}()

		utils.LogPrintf("[IMG] Attempting to restart processor...")

		// 先清理旧进程
		if p.cmd != nil && p.cmd.Process != nil {
			p.cmd.Process.Kill()
			p.cmd.Wait()
		}

		// 重新启动
		p.startProcessor()
	}()
}

// checkAndRestart 检查进程状态，必要时重启
func (p *ImgProcessor) checkAndRestart() {
	// 检查进程是否还活着
	if p.cmd == nil || p.cmd.Process == nil {
		p.tryRestart()
		return
	}

	// 检查 socket 是否存在
	if _, err := os.Stat(SocketPath); os.IsNotExist(err) {
		p.tryRestart()
		return
	}
}

// ToWebP 将图片转换为 WebP 格式
func (p *ImgProcessor) ToWebP(imageData []byte) ([]byte, error) {
	if len(imageData) == 0 {
		return nil, errors.New("empty image data")
	}
	if len(imageData) > MaxImageSize {
		return nil, ErrImageTooLarge
	}

	// 获取信号量（限制并发）
	p.sem <- struct{}{}
	defer func() { <-p.sem }()

	// 连接
	conn, err := net.DialTimeout("unix", SocketPath, ConnectTimeout)
	if err != nil {
		p.available = false
		p.checkAndRestart() // 连接失败时触发重启检查
		return nil, fmt.Errorf("%w: %v", ErrProcessorNotAvailable, err)
	}
	defer conn.Close()

	// 设置超时
	deadline := time.Now().Add(ReadWriteTimeout)
	conn.SetDeadline(deadline)

	// 发送: [4字节长度][数据]
	lenBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBuf, uint32(len(imageData)))
	if _, err := conn.Write(lenBuf); err != nil {
		return nil, fmt.Errorf("write length failed: %w", err)
	}
	if _, err := conn.Write(imageData); err != nil {
		return nil, fmt.Errorf("write data failed: %w", err)
	}

	// 读取响应: [1字节状态][4字节长度][数据]
	statusBuf := make([]byte, 1)
	if _, err := conn.Read(statusBuf); err != nil {
		return nil, fmt.Errorf("read status failed: %w", err)
	}

	if _, err := conn.Read(lenBuf); err != nil {
		return nil, fmt.Errorf("read length failed: %w", err)
	}
	respLen := binary.BigEndian.Uint32(lenBuf)

	if respLen > MaxImageSize {
		return nil, errors.New("response too large")
	}

	respData := make([]byte, respLen)
	if err := readFull(conn, respData); err != nil {
		return nil, fmt.Errorf("read data failed: %w", err)
	}

	// 检查状态
	if statusBuf[0] != 0 {
		return nil, fmt.Errorf("%w: %s", ErrProcessFailed, string(respData))
	}

	p.available = true
	return respData, nil
}

// readFull 完整读取数据
func readFull(conn net.Conn, buf []byte) error {
	total := 0
	for total < len(buf) {
		n, err := conn.Read(buf[total:])
		if err != nil {
			return err
		}
		total += n
	}
	return nil
}
