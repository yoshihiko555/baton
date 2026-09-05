package hook

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"io/fs"
	"log"
	"net"
	"syscall"
	"time"

	"github.com/yoshihiko555/baton/internal/config"
)

// Send は hook イベントを Unix domain socket へ送信する。
func Send(cfg config.HookConfig, payload []byte) error {
	conn, err := net.DialTimeout("unix", cfg.SocketPath, time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()

	if err := conn.SetWriteDeadline(time.Now().Add(time.Second)); err != nil {
		return err
	}

	message := make([]byte, len(payload)+1)
	copy(message, payload)
	message[len(payload)] = '\n'
	_, err = conn.Write(message)
	return err
}

// RunHookCommand は baton hook サブコマンドを実行する。
// Claude Code の実行を妨げないよう、すべての経路で終了コード 0 を返す。
func RunHookCommand(args []string, stdin io.Reader, getenv func(string) string) int {
	flags := flag.NewFlagSet("hook", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String("config", "", "path to config file")
	if err := flags.Parse(args); err != nil {
		log.Printf("hook: parse flags: %v", err)
		return 0
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Printf("hook: load config: %v", err)
		return 0
	}
	if !cfg.Hook.Enabled {
		return 0
	}

	paneID := getenv("TMUX_PANE")
	if paneID == "" {
		return 0
	}

	input, err := io.ReadAll(stdin)
	if err != nil {
		log.Printf("hook: read stdin: %v", err)
		return 0
	}

	var ev Event
	if err := json.Unmarshal(input, &ev); err != nil {
		log.Printf("hook: decode stdin: %v", err)
		return 0
	}
	if bytes.Equal(bytes.TrimSpace(input), []byte("null")) {
		log.Printf("hook: decode stdin: expected a JSON object")
		return 0
	}
	ev.PaneID = paneID

	payload, err := json.Marshal(ev)
	if err != nil {
		log.Printf("hook: encode event: %v", err)
		return 0
	}
	if err := Send(cfg.Hook, payload); err != nil && !errors.Is(err, syscall.ECONNREFUSED) && !errors.Is(err, fs.ErrNotExist) {
		log.Printf("hook: send event: %v", err)
	}
	return 0
}
