package services

import (
	"bytes"
	"context"
	"crypto/sha256"
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
	// imgProcessorDirName 私有临时目录名前缀，权限 0700，防止其他用户写入或预置符号链接
	imgProcessorDirName = "nebula-imgproc"
	// imgProcessorFilePrefix 临时二进制文件名前缀，配合 os.CreateTemp 生成随机后缀
	imgProcessorFilePrefix = "proc-"
	ConnectTimeout         = 5 * time.Second
	ReadWriteTimeout       = 30 * time.Second
	MaxImageSize           = 10 * 1024 * 1024
	MaxConcurrent          = 2
)

var (
	ErrProcessorNotAvailable = errors.New("image processor not available")
	ErrImageTooLarge         = errors.New("image too large")
	ErrProcessFailed         = errors.New("image process failed")
)

// 嵌入 Zig 二进制（编译时由 GitHub Actions 放置）
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
	socketPath string        // Unix Socket 路径
	binaryPath string        // 已写入的二进制路径（每次启动随机生成）
	tempDir    string        // 私有临时目录路径
}

// NewImgProcessor 创建图片处理服务
func NewImgProcessor(socketPath string) *ImgProcessor {
	p := &ImgProcessor{
		sem:        make(chan struct{}, MaxConcurrent),
		socketPath: socketPath,
	}
	p.startProcessor()
	return p
}

// startProcessor 启动 Zig 处理器
func (p *ImgProcessor) startProcessor() {
	// 清理上次启动遗留的临时目录（如重启场景）
	if p.tempDir != "" {
		os.RemoveAll(p.tempDir)
		p.tempDir = ""
		p.binaryPath = ""
	}

	if len(imgProcessorBin) == 0 {
		utils.LogWarn("IMG", "Embedded binary not found, using fallback", "")
		p.available = false
		return
	}

	expectedHash := sha256.Sum256(imgProcessorBin)

	// 创建权限 0700 的私有临时目录，防止其他用户预置符号链接或写入文件
	tempDir, err := os.MkdirTemp("", imgProcessorDirName+"-*")
	if err != nil {
		utils.LogError("IMG", "start", err, "Failed to create private temp dir")
		p.available = false
		return
	}
	if err := os.Chmod(tempDir, 0o700); err != nil {
		utils.LogError("IMG", "start", err, "Failed to chmod temp dir")
		os.RemoveAll(tempDir)
		p.available = false
		return
	}
	p.tempDir = tempDir

	// 在私有目录内用 os.CreateTemp 生成随机文件名的二进制文件。
	// CreateTemp 内部使用 O_CREAT|O_EXCL（不覆盖已有文件，不跟随符号链接），
	// 配合 0700 私有目录和随机文件名，阻断符号链接劫持。
	tempFile, err := os.CreateTemp(tempDir, imgProcessorFilePrefix)
	if err != nil {
		utils.LogError("IMG", "start", err, "Failed to create temp binary file")
		os.RemoveAll(tempDir)
		p.available = false
		return
	}
	binaryPath := tempFile.Name()

	if _, err := tempFile.Write(imgProcessorBin); err != nil {
		utils.LogError("IMG", "start", err, "Failed to write binary")
		tempFile.Close()
		os.RemoveAll(tempDir)
		p.available = false
		return
	}
	if err := tempFile.Close(); err != nil {
		utils.LogError("IMG", "start", err, "Failed to close binary file")
		os.RemoveAll(tempDir)
		p.available = false
		return
	}
	// 显式设置权限为 0700（仅属主可读写执行）
	if err := os.Chmod(binaryPath, 0o700); err != nil {
		utils.LogError("IMG", "start", err, "Failed to chmod binary")
		os.RemoveAll(tempDir)
		p.available = false
		return
	}
	p.binaryPath = binaryPath

	// 写入后校验哈希，确认内容完整（此时文件已安全写入，校验失败直接清理）
	writtenData, err := os.ReadFile(binaryPath)
	if err != nil {
		utils.LogError("IMG", "start", err, "Failed to verify binary integrity")
		os.RemoveAll(tempDir)
		p.available = false
		return
	}
	actualHash := sha256.Sum256(writtenData)
	if !bytes.Equal(expectedHash[:], actualHash[:]) {
		utils.LogError("IMG", "start", nil, "Binary integrity check failed: hash mismatch")
		os.RemoveAll(tempDir)
		p.available = false
		return
	}

	os.Remove(p.socketPath)

	p.cmd = exec.Command(binaryPath)
	if err := p.cmd.Start(); err != nil {
		utils.LogError("IMG", "start", err, "Failed to start processor")
		os.RemoveAll(tempDir)
		p.available = false
		return
	}

	for range 50 {
		time.Sleep(100 * time.Millisecond)
		if _, err := os.Stat(p.socketPath); err == nil {
			p.available = true
			utils.LogInfo("IMG", fmt.Sprintf("Image processor started (PID: %d)", p.cmd.Process.Pid))
			return
		}
	}

	utils.LogWarn("IMG", "Processor started but socket not ready", "")
	p.available = false
}

// Shutdown 关闭处理器
func (p *ImgProcessor) Shutdown(ctx context.Context) {
	done := make(chan struct{})
	go func() {
		if p.cmd != nil && p.cmd.Process != nil {
			p.cmd.Process.Kill()
			p.cmd.Wait()
			utils.LogInfo("IMG", "Image processor stopped")
		}
		os.Remove(p.socketPath)
		// 清理私有临时目录及其中的二进制文件
		if p.tempDir != "" {
			os.RemoveAll(p.tempDir)
		}
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		utils.LogWarn("IMG", "Shutdown timeout exceeded, forcing shutdown", "")
	}
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

		utils.LogInfo("IMG", "Attempting to restart processor...")

		if p.cmd != nil && p.cmd.Process != nil {
			p.cmd.Process.Kill()
			p.cmd.Wait()
		}

		p.startProcessor()
	}()
}

// checkAndRestart 检查进程状态，必要时重启
func (p *ImgProcessor) checkAndRestart() {
	if p.cmd == nil || p.cmd.Process == nil {
		p.tryRestart()
		return
	}

	if _, err := os.Stat(p.socketPath); os.IsNotExist(err) {
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

	p.sem <- struct{}{}
	defer func() { <-p.sem }()

	conn, err := net.DialTimeout("unix", p.socketPath, ConnectTimeout)
	if err != nil {
		p.available = false
		p.checkAndRestart() // 连接失败时触发重启检查
		return nil, fmt.Errorf("%w: %v", ErrProcessorNotAvailable, err)
	}
	defer conn.Close()

	deadline := time.Now().Add(ReadWriteTimeout)
	conn.SetDeadline(deadline)

	lenBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBuf, uint32(len(imageData)))
	if _, err := conn.Write(lenBuf); err != nil {
		return nil, fmt.Errorf("write length failed: %w", err)
	}
	if _, err := conn.Write(imageData); err != nil {
		return nil, fmt.Errorf("write data failed: %w", err)
	}

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
