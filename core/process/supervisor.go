package process

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

// Supervisor 以受管子进程方式运行 sing-box，支持启停、重启、自动拉起与日志采集。
type Supervisor struct {
	mu         sync.Mutex
	binPath    string
	configPath string

	cmd    *exec.Cmd
	cancel context.CancelFunc
	exitCh chan struct{}

	running bool
	stopped bool

	logs         *RingBuffer
	restartDelay time.Duration
	onExit       func(err error)
}

// New 创建进程监管器。
func New(binPath, configPath string, logCap int) *Supervisor {
	return &Supervisor{
		binPath:      binPath,
		configPath:   configPath,
		logs:         NewRingBuffer(logCap),
		restartDelay: 2 * time.Second,
	}
}

// SetOnExit 注册进程退出回调（非手动停止时也会触发）。
func (s *Supervisor) SetOnExit(f func(err error)) {
	s.mu.Lock()
	s.onExit = f
	s.mu.Unlock()
}

// Logs 返回内核日志快照。
func (s *Supervisor) Logs() []string { return s.logs.Lines() }

// ClearLogs 清空内核日志。
func (s *Supervisor) ClearLogs() { s.logs.Clear() }

// Running 进程是否在运行。
func (s *Supervisor) Running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// Start 启动内核进程。
func (s *Supervisor) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.startLocked()
}

func (s *Supervisor) startLocked() error {
	if s.running {
		return nil
	}
	if _, err := os.Stat(s.binPath); err != nil {
		return fmt.Errorf("内核不存在: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, s.binPath, "run", "-c", s.configPath)
	cmd.Env = os.Environ()

	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw

	if err := cmd.Start(); err != nil {
		cancel()
		pw.Close()
		pr.Close()
		return err
	}

	s.cmd = cmd
	s.cancel = cancel
	s.exitCh = make(chan struct{})
	s.running = true
	s.stopped = false

	go s.pump(pr)
	go s.wait(cmd, pw, s.exitCh)
	return nil
}

func (s *Supervisor) pump(r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		s.logs.Add(sc.Text())
	}
}

func (s *Supervisor) wait(cmd *exec.Cmd, pw *io.PipeWriter, exitCh chan struct{}) {
	err := cmd.Wait()
	pw.Close()

	s.mu.Lock()
	s.running = false
	s.cmd = nil
	stopped := s.stopped
	onExit := s.onExit
	close(exitCh)
	s.mu.Unlock()

	if onExit != nil {
		onExit(err)
	}

	if !stopped {
		s.logs.Add(fmt.Sprintf("[horseui] 内核意外退出: %v，%s 后自动重启", err, s.restartDelay))
		time.Sleep(s.restartDelay)
		s.mu.Lock()
		if !s.stopped {
			_ = s.startLocked()
		}
		s.mu.Unlock()
	}
}

// Stop 停止内核进程（不再自动重启）。
func (s *Supervisor) Stop() error {
	s.mu.Lock()
	s.stopped = true
	if !s.running {
		s.mu.Unlock()
		return nil
	}
	cancel := s.cancel
	exitCh := s.exitCh
	cmd := s.cmd
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Signal(os.Interrupt)
	}
	if exitCh != nil {
		select {
		case <-exitCh:
		case <-time.After(5 * time.Second):
			if cmd != nil && cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			select {
			case <-exitCh:
			case <-time.After(3 * time.Second):
			}
		}
	}
	return nil
}

// Restart 重启内核进程。
func (s *Supervisor) Restart() error {
	if err := s.Stop(); err != nil {
		return err
	}
	time.Sleep(300 * time.Millisecond)
	return s.Start()
}
