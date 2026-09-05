package hook

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const defaultMaxConnections = 32

// ServerOptions は hook サーバーの起動設定を表す。
type ServerOptions struct {
	SocketPath          string
	Store               *Store
	OnPermissionRequest func()
	Debounce            time.Duration
	MaxConnections      int
	ReadDeadline        time.Duration
}

// ErrAlreadyListening は別の baton が同じ hook socket を使用中であることを示す。
var ErrAlreadyListening = errors.New("hook socket already in use by another instance")

// Server は Claude Code hook イベントを受信する Unix domain socket サーバーである。
type Server struct {
	socketPath          string
	listener            net.Listener
	store               *Store
	onPermissionRequest func()
	debounce            time.Duration
	readDeadline        time.Duration
	sem                 chan struct{}

	closing atomic.Bool
	wg      sync.WaitGroup

	timerMu         sync.Mutex
	debounceTimer   *time.Timer
	debounceVersion uint64

	closeOnce sync.Once
	closeErr  error
}

// Listen は hook socket を作成し、イベントの受信を開始する。
func Listen(opts ServerOptions) (*Server, error) {
	if err := os.MkdirAll(filepath.Dir(opts.SocketPath), 0o700); err != nil {
		return nil, fmt.Errorf("create hook socket directory: %w", err)
	}
	if opts.Debounce <= 0 {
		opts.Debounce = 500 * time.Millisecond
	}
	if opts.MaxConnections <= 0 {
		opts.MaxConnections = defaultMaxConnections
	}
	if opts.ReadDeadline <= 0 {
		opts.ReadDeadline = 5 * time.Second
	}
	if opts.Store == nil {
		opts.Store = NewStore(0)
	}

	fi, err := os.Lstat(opts.SocketPath)
	if err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("hook socket path %q is a symlink; refusing to remove", opts.SocketPath)
		}
		st, ok := fi.Sys().(*syscall.Stat_t)
		if !ok {
			return nil, fmt.Errorf("hook socket path %q: could not determine file owner", opts.SocketPath)
		}
		if int(st.Uid) != os.Getuid() {
			return nil, fmt.Errorf("hook socket path %q は別ユーザー所有のためスキップします (uid=%d)", opts.SocketPath, st.Uid)
		}
		if fi.Mode()&os.ModeSocket == 0 {
			return nil, fmt.Errorf("hook socket path %q exists and is not a socket; refusing to remove", opts.SocketPath)
		}

		conn, dialErr := net.DialTimeout("unix", opts.SocketPath, time.Second)
		if dialErr == nil {
			_ = conn.Close()
			return nil, ErrAlreadyListening
		}
		if err := os.Remove(opts.SocketPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("remove stale hook socket: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("lstat hook socket: %w", err)
	}

	listener, err := net.Listen("unix", opts.SocketPath)
	if err != nil {
		return nil, fmt.Errorf("listen on hook socket: %w", err)
	}
	if err := os.Chmod(opts.SocketPath, 0o600); err != nil {
		_ = listener.Close()
		_ = os.Remove(opts.SocketPath)
		return nil, fmt.Errorf("chmod hook socket: %w", err)
	}

	server := &Server{
		socketPath:          opts.SocketPath,
		listener:            listener,
		store:               opts.Store,
		onPermissionRequest: opts.OnPermissionRequest,
		debounce:            opts.Debounce,
		readDeadline:        opts.ReadDeadline,
		sem:                 make(chan struct{}, opts.MaxConnections),
	}
	server.wg.Add(1)
	go server.acceptLoop()
	return server, nil
}

func (s *Server) acceptLoop() {
	defer s.wg.Done()
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if s.closing.Load() {
				return
			}
			log.Printf("hook server: accept connection: %v", err)
			continue
		}
		select {
		case s.sem <- struct{}{}:
		default:
			_ = conn.Close()
			continue
		}

		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.handleConn(conn)
		}()
	}
}

func (s *Server) handleConn(conn net.Conn) {
	defer conn.Close()
	defer func() { <-s.sem }()
	if err := conn.SetReadDeadline(time.Now().Add(s.readDeadline)); err != nil {
		log.Printf("hook server: set read deadline: %v", err)
		return
	}

	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		ev, err := ParseEvent(scanner.Bytes())
		if err != nil {
			log.Printf("hook server: parse event: %v", err)
			continue
		}
		s.store.Apply(ev)
		if ev.HookEventName == "PermissionRequest" && s.onPermissionRequest != nil {
			s.triggerPermissionRequest()
		}
	}
	if err := scanner.Err(); shouldLogConnectionError(err) {
		log.Printf("hook server: read connection: %v", err)
	}
}

func shouldLogConnectionError(err error) bool {
	if err == nil || errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) || errors.Is(err, os.ErrDeadlineExceeded) {
		return false
	}
	var netErr net.Error
	return !errors.As(err, &netErr) || !netErr.Timeout()
}

func (s *Server) triggerPermissionRequest() {
	s.timerMu.Lock()
	defer s.timerMu.Unlock()
	if s.closing.Load() {
		return
	}

	s.debounceVersion++
	version := s.debounceVersion
	if s.debounceTimer != nil {
		s.debounceTimer.Stop()
	}
	s.debounceTimer = time.AfterFunc(s.debounce, func() {
		s.timerMu.Lock()
		if s.closing.Load() || version != s.debounceVersion {
			s.timerMu.Unlock()
			return
		}
		s.debounceTimer = nil
		s.timerMu.Unlock()
		s.onPermissionRequest()
	})
}

// Close は受信を停止し、hook socket を削除する。
func (s *Server) Close() error {
	s.closeOnce.Do(func() {
		s.closing.Store(true)

		s.timerMu.Lock()
		if s.debounceTimer != nil {
			s.debounceTimer.Stop()
			s.debounceTimer = nil
		}
		s.timerMu.Unlock()

		s.closeErr = s.listener.Close()
		if err := os.Remove(s.socketPath); err != nil && !errors.Is(err, os.ErrNotExist) && s.closeErr == nil {
			s.closeErr = fmt.Errorf("remove hook socket: %w", err)
		}
		s.wg.Wait()
	})
	return s.closeErr
}
