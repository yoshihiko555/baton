package core

import (
	"context"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

const debounceDuration = 200 * time.Millisecond

// Watcher はセッションファイルを監視し、正規化したイベントを配信する。
type Watcher struct {
	watcher        *fsnotify.Watcher
	basePath       string
	events         chan WatchEvent
	done           chan struct{}
	mu             sync.Mutex
	debounceTimers map[string]*time.Timer
	stopOnce       sync.Once
}

// NewWatcher は basePath 配下を監視する Watcher を生成する。
func NewWatcher(basePath string) (*Watcher, error) {
	absBasePath, err := filepath.Abs(basePath)
	if err != nil {
		return nil, err
	}

	info, err := os.Stat(absBasePath)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, &os.PathError{Op: "new watcher", Path: absBasePath, Err: os.ErrInvalid}
	}

	fsWatcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	return &Watcher{
		watcher:        fsWatcher,
		basePath:       absBasePath,
		events:         make(chan WatchEvent, 256),
		done:           make(chan struct{}),
		debounceTimers: make(map[string]*time.Timer),
	}, nil
}

// Start は監視開始と初期探索イベントの送出を行う。
func (w *Watcher) Start(ctx context.Context) error {
	if err := w.addRecursive(w.basePath); err != nil {
		return err
	}

	go func() {
		for {
			select {
			case <-w.done:
				return
			case <-ctx.Done():
				w.Stop()
				return
			case event, ok := <-w.watcher.Events:
				if !ok {
					return
				}

				if event.Op&fsnotify.Create == fsnotify.Create {
					info, err := os.Stat(event.Name)
					if err == nil && info.IsDir() {
						_ = w.addRecursive(event.Name)
						continue
					}
				}

				watchEvent, ok := w.pathToWatchEvent(event.Name, event.Op)
				if !ok {
					continue
				}

				captured := watchEvent // クロージャで参照するためループ変数を退避する
				key := captured.Type.String() + ":" + filepath.ToSlash(captured.Path)
				w.debounce(key, func() {
					select {
					case <-w.done:
						return
					case w.events <- captured:
					default:
						log.Printf("watcher: event channel full, dropping %s", captured.Path)
					}
				})
			case watchErr, ok := <-w.watcher.Errors:
				if !ok {
					return
				}
				log.Printf("watcher error: %v", watchErr)
			}
		}
	}()

	return nil
}

// Stop は監視と保留中デバウンスタイマーを停止する。
// 複数回呼び出しても安全。
func (w *Watcher) Stop() {
	w.stopOnce.Do(func() {
		close(w.done)

		w.mu.Lock()
		for key, timer := range w.debounceTimers {
			timer.Stop()
			delete(w.debounceTimers, key)
		}
		w.mu.Unlock()

		_ = w.watcher.Close()
		close(w.events)
	})
}

// Events は読み取り専用イベントチャネルを返す。
func (w *Watcher) Events() <-chan WatchEvent {
	return w.events
}

// DiscoverProjects はセッションファイルを含むプロジェクトディレクトリを列挙する。
func (w *Watcher) DiscoverProjects() ([]string, error) {
	entries, err := os.ReadDir(w.basePath)
	if err != nil {
		return nil, err
	}

	projects := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		projectPath := filepath.Join(w.basePath, entry.Name())
		sessions, err := w.DiscoverSessions(projectPath)
		if err != nil {
			return nil, err
		}
		if len(sessions) == 0 {
			continue
		}

		projects = append(projects, projectPath)
	}

	return projects, nil
}

// DiscoverSessions はプロジェクト配下のセッション ID を列挙する。
func (w *Watcher) DiscoverSessions(projectPath string) ([]string, error) {
	absProjectPath, err := filepath.Abs(projectPath)
	if err != nil {
		return nil, err
	}

	sessions := []string{}
	err = filepath.Walk(absProjectPath, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if info.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(absProjectPath, filePath)
		if err != nil {
			return err
		}
		relPath = filepath.ToSlash(relPath)
		if !isSessionFile(relPath) {
			return nil
		}

		ext := path.Ext(relPath)
		sessionID := strings.TrimSuffix(relPath, ext)
		sessions = append(sessions, sessionID)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return sessions, nil
}

func (w *Watcher) debounce(key string, fn func()) {
	w.mu.Lock()
	if existing, ok := w.debounceTimers[key]; ok {
		// 同一キーの既存タイマーを止め、最後のイベントのみを残す。
		existing.Stop()
	}

	var timer *time.Timer
	timer = time.AfterFunc(debounceDuration, func() {
		fn()

		w.mu.Lock()
		if w.debounceTimers[key] == timer {
			delete(w.debounceTimers, key)
		}
		w.mu.Unlock()
	})

	w.debounceTimers[key] = timer
	w.mu.Unlock()
}

func (w *Watcher) pathToWatchEvent(filePath string, op fsnotify.Op) (WatchEvent, bool) {
	eventType, ok := opToWatchEventType(op)
	if !ok {
		return WatchEvent{}, false
	}

	absFilePath := filepath.Clean(filePath)
	if !filepath.IsAbs(absFilePath) {
		absFilePath = filepath.Join(w.basePath, absFilePath)
	}

	relPath, err := filepath.Rel(w.basePath, absFilePath)
	if err != nil {
		return WatchEvent{}, false
	}
	relPath = filepath.ToSlash(relPath)
	if relPath == "." || relPath == ".." || strings.HasPrefix(relPath, "../") {
		// basePath 外や無効相対パスは無視する。
		return WatchEvent{}, false
	}
	if !isSessionFile(relPath) {
		return WatchEvent{}, false
	}

	segments := strings.Split(relPath, "/")
	if len(segments) < 2 {
		// <project>/<session-file> 形式でないパスは対象外。
		return WatchEvent{}, false
	}

	projectPath := filepath.Join(w.basePath, segments[0])

	ext := path.Ext(relPath)
	sessionPath := strings.TrimSuffix(relPath, ext)
	sessionSegments := strings.Split(sessionPath, "/")
	if len(sessionSegments) < 2 {
		return WatchEvent{}, false
	}
	sessionID := strings.Join(sessionSegments[1:], "/")

	return WatchEvent{
		Type:        eventType,
		Path:        absFilePath,
		ProjectPath: projectPath,
		SessionID:   sessionID,
	}, true
}

func (w *Watcher) addRecursive(root string) error {
	return filepath.Walk(root, func(currentPath string, info os.FileInfo, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if !info.IsDir() {
			return nil
		}

		if err := w.watcher.Add(currentPath); err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}

		return nil
	})
}

func opToWatchEventType(op fsnotify.Op) (WatchEventType, bool) {
	// Rename は実体的には削除扱いとして集約側で処理する。
	switch {
	case op&(fsnotify.Remove|fsnotify.Rename) != 0:
		return Removed, true
	case op&fsnotify.Create != 0:
		return Created, true
	case op&(fsnotify.Write|fsnotify.Chmod) != 0:
		return Modified, true
	default:
		return 0, false
	}
}

func isSessionFile(filePath string) bool {
	ext := strings.ToLower(path.Ext(filepath.ToSlash(filePath)))
	return ext == ".jsonl" || ext == ".json"
}
