package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/rpc"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	exitAlreadyRunning = 10
	exitStartTimeout   = 11
	exitSpawnFailure   = 12

	exitNotRunning  = 20
	exitStopTimeout = 21

	exitStopped     = 30
	exitRPCUnhealth = 31
)

type fileConfig struct {
	Transport       string   `json:"transport"`
	HTTPAddr        string   `json:"http_addr"`
	P2PListen       []string `json:"p2p_listen"`
	P2PBootstrap    []string `json:"p2p_bootstrap"`
	P2PMDNS         *bool    `json:"p2p_mdns"`
	P2PRendezvous   string   `json:"p2p_rendezvous"`
	P2PIdentityKey  string   `json:"p2p_identity_key"`
	P2PRecentPeers  string   `json:"p2p_recent_peers"`
	LocalRPCEnable  *bool    `json:"local_rpc_enable"`
	LocalRPCSock    string   `json:"local_rpc_sock"`
	LocalRPCRecords string   `json:"local_rpc_records"`
	LocalRPCCursors string   `json:"local_rpc_cursors"`
	RunPIDFile      string   `json:"run_pid_file"`
	RunLogFile      string   `json:"run_log_file"`
}

type runtimeConfig struct {
	Transport       string
	HTTPAddr        string
	P2PListen       []string
	P2PBootstrap    []string
	P2PMDNS         bool
	P2PRendezvous   string
	P2PIdentityKey  string
	P2PRecentPeers  string
	LocalRPCEnable  bool
	LocalRPCSock    string
	LocalRPCRecords string
	LocalRPCCursors string
	RunPIDFile      string
	RunLogFile      string
}

type statusArgs struct{}

type statusReply struct {
	Transport           string
	PeerID              string
	ConnectedPeers      int
	ListenAddrs         []string
	ConnectedPeerIDs    []string
	ConnectedPeerAddrs  []string
	StartedAt           time.Time
	ActiveSubscriptions int
	MessagesPublished   int64
	MessagesInNetwork   int64
	MessagesInStream    int64
	MessagesFanout      int64
	DirectSends         int64
	Error               string
}

type statusOutput struct {
	Process             string    `json:"process"`
	PID                 int       `json:"pid,omitempty"`
	RPCSocket           string    `json:"rpc_socket"`
	RPCSocketExist      bool      `json:"rpc_socket_exists"`
	Transport           string    `json:"transport,omitempty"`
	PeerID              string    `json:"peer_id,omitempty"`
	ConnectedPeers      int       `json:"connected_peers,omitempty"`
	ListenAddrs         []string  `json:"listen_addrs,omitempty"`
	ConnectedPeerIDs    []string  `json:"connected_peer_ids,omitempty"`
	ConnectedPeerAddrs  []string  `json:"connected_peer_addrs,omitempty"`
	ActiveSubscriptions int       `json:"active_subscriptions,omitempty"`
	MessagesPublished   int64     `json:"messages_published,omitempty"`
	MessagesInNetwork   int64     `json:"messages_in_network,omitempty"`
	MessagesInStream    int64     `json:"messages_in_stream,omitempty"`
	MessagesFanout      int64     `json:"messages_fanout,omitempty"`
	DirectSends         int64     `json:"direct_sends,omitempty"`
	StartedAt           time.Time `json:"started_at,omitempty"`
	UptimeSec           int64     `json:"uptime_sec,omitempty"`
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	switch os.Args[1] {
	case "start":
		os.Exit(runStart(os.Args[2:]))
	case "stop":
		os.Exit(runStop(os.Args[2:]))
	case "status":
		os.Exit(runStatus(os.Args[2:]))
	case "logs":
		os.Exit(runLogs(os.Args[2:]))
	case "rpc":
		if len(os.Args) >= 3 && os.Args[2] == "status" {
			os.Exit(runRPCStatus(os.Args[3:]))
		}
		usage()
		os.Exit(2)
	case "-h", "--help", "help":
		usage()
		os.Exit(0)
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Println("assemblerctl <command>")
	fmt.Println("commands:")
	fmt.Println("  start       start assembler daemon")
	fmt.Println("  stop        stop assembler daemon")
	fmt.Println("  status      show process + rpc status")
	fmt.Println("  logs        print/tail daemon logs")
	fmt.Println("  rpc status  query P2P.GetStatus over local rpc")
}

func runStart(args []string) int {
	fs := flag.NewFlagSet("start", flag.ContinueOnError)
	configPath := fs.String("config", filepath.Join("data", "assembler.json"), "config file path")
	workdir := fs.String("workdir", ".", "working directory for daemon process")
	daemon := fs.Bool("daemon", true, "run in daemon mode")
	wait := fs.Duration("wait", 8*time.Second, "startup health timeout")
	daemonBin := fs.String("daemon-bin", "", "optional assembler daemon binary path")
	serverBinCompat := fs.String("server-bin", "", "deprecated alias of --daemon-bin")
	pidOverride := fs.String("pid-file", "", "pid file path override")
	logOverride := fs.String("log-file", "", "log file path override")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	cfg, err := loadConfig(*configPath)
	if err != nil {
		fmt.Printf("load config failed: %v\n", err)
		return exitSpawnFailure
	}
	if *pidOverride != "" {
		cfg.RunPIDFile = *pidOverride
	}
	if *logOverride != "" {
		cfg.RunLogFile = *logOverride
	}
	if err := ensureDir(filepath.Dir(cfg.RunPIDFile), 0o700); err != nil {
		fmt.Printf("prepare pid dir failed: %v\n", err)
		return exitSpawnFailure
	}
	if err := ensureDir(filepath.Dir(cfg.RunLogFile), 0o700); err != nil {
		fmt.Printf("prepare log dir failed: %v\n", err)
		return exitSpawnFailure
	}
	if err := ensureDir(filepath.Dir(cfg.LocalRPCSock), 0o700); err != nil {
		fmt.Printf("prepare rpc dir failed: %v\n", err)
		return exitSpawnFailure
	}

	pid, _, err := readPID(cfg.RunPIDFile)
	if err == nil && processAlive(pid) {
		fmt.Printf("already running: pid=%d\n", pid)
		return exitAlreadyRunning
	}
	if err == nil {
		_ = os.Remove(cfg.RunPIDFile)
	}

	logf, err := os.OpenFile(cfg.RunLogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		fmt.Printf("open log file failed: %v\n", err)
		return exitSpawnFailure
	}
	defer logf.Close()

	serverArgs := buildServerArgs(cfg)
	var cmd *exec.Cmd
	daemonPath := strings.TrimSpace(*daemonBin)
	if daemonPath == "" {
		daemonPath = strings.TrimSpace(*serverBinCompat)
	}
	if daemonPath != "" {
		cmd = exec.Command(daemonPath, serverArgs...)
	} else {
		cmd = exec.Command("go", append([]string{"run", "./cmd/assemblerd"}, serverArgs...)...)
	}
	cmd.Dir = *workdir
	if !*daemon {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin
		if err := cmd.Run(); err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				return exitErr.ExitCode()
			}
			fmt.Printf("foreground start failed: %v\n", err)
			return exitSpawnFailure
		}
		return 0
	}
	cmd.Stdout = logf
	cmd.Stderr = logf
	cmd.Stdin = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		fmt.Printf("spawn failed: %v\n", err)
		return exitSpawnFailure
	}
	pid = cmd.Process.Pid
	started := time.Now().UTC()
	if err := writePID(cfg.RunPIDFile, pid, started); err != nil {
		_ = cmd.Process.Kill()
		fmt.Printf("write pid file failed: %v\n", err)
		return exitSpawnFailure
	}
	_ = cmd.Process.Release()

	deadline := time.Now().Add(*wait)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			_ = os.Remove(cfg.RunPIDFile)
			fmt.Printf("daemon exited early, check logs: %s\n", cfg.RunLogFile)
			return exitSpawnFailure
		}
		rep, err := rpcGetStatus(cfg.LocalRPCSock)
		if err == nil && rep.Error == "" {
			fmt.Printf("started pid=%d peer_id=%s rpc=%s\n", pid, rep.PeerID, cfg.LocalRPCSock)
			return 0
		}
		time.Sleep(300 * time.Millisecond)
	}

	_ = syscall.Kill(pid, syscall.SIGTERM)
	_ = os.Remove(cfg.RunPIDFile)
	fmt.Printf("start timeout after %s, stopped pid=%d\n", wait.String(), pid)
	return exitStartTimeout
}

func runStop(args []string) int {
	fs := flag.NewFlagSet("stop", flag.ContinueOnError)
	configPath := fs.String("config", filepath.Join("data", "assembler.json"), "config file path")
	pidOverride := fs.String("pid-file", "", "pid file path override")
	timeout := fs.Duration("timeout", 10*time.Second, "graceful stop timeout")
	force := fs.Bool("force", false, "force kill on timeout")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	cfg, err := loadConfig(*configPath)
	if err != nil {
		fmt.Printf("load config failed: %v\n", err)
		return exitNotRunning
	}
	if *pidOverride != "" {
		cfg.RunPIDFile = *pidOverride
	}

	pid, _, err := readPID(cfg.RunPIDFile)
	if err != nil || !processAlive(pid) {
		_ = os.Remove(cfg.RunPIDFile)
		fmt.Println("not running")
		return exitNotRunning
	}

	_ = syscall.Kill(pid, syscall.SIGTERM)
	deadline := time.Now().Add(*timeout)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			_ = os.Remove(cfg.RunPIDFile)
			fmt.Printf("stopped pid=%d\n", pid)
			return 0
		}
		time.Sleep(200 * time.Millisecond)
	}
	if *force {
		_ = syscall.Kill(pid, syscall.SIGKILL)
		time.Sleep(200 * time.Millisecond)
		if !processAlive(pid) {
			_ = os.Remove(cfg.RunPIDFile)
			fmt.Printf("force-stopped pid=%d\n", pid)
			return 0
		}
	}
	fmt.Printf("stop timeout pid=%d\n", pid)
	return exitStopTimeout
}

func runStatus(args []string) int {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	configPath := fs.String("config", filepath.Join("data", "assembler.json"), "config file path")
	pidOverride := fs.String("pid-file", "", "pid file path override")
	rpcOverride := fs.String("rpc-sock", "", "rpc socket override")
	jsonOut := fs.Bool("json", false, "print json")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	cfg, err := loadConfig(*configPath)
	if err != nil {
		fmt.Printf("load config failed: %v\n", err)
		return exitStopped
	}
	if *pidOverride != "" {
		cfg.RunPIDFile = *pidOverride
	}
	if *rpcOverride != "" {
		cfg.LocalRPCSock = *rpcOverride
	}

	pid, startedAt, pidErr := readPID(cfg.RunPIDFile)
	out := statusOutput{
		Process:        "stopped",
		PID:            0,
		RPCSocket:      cfg.LocalRPCSock,
		RPCSocketExist: fileExists(cfg.LocalRPCSock),
	}
	if pidErr == nil {
		out.PID = pid
	}
	if !startedAt.IsZero() {
		out.StartedAt = startedAt
		out.UptimeSec = int64(time.Since(startedAt).Seconds())
	}
	if pidErr == nil {
		if processAlive(pid) {
			out.Process = "running"
		} else {
			out.Process = "stale_pid"
		}
	}

	rep, rpcErr := rpcGetStatus(cfg.LocalRPCSock)
	if rpcErr == nil && rep.Error == "" {
		out.Transport = rep.Transport
		out.PeerID = rep.PeerID
		out.ConnectedPeers = rep.ConnectedPeers
		out.ListenAddrs = rep.ListenAddrs
		out.ConnectedPeerIDs = rep.ConnectedPeerIDs
		out.ConnectedPeerAddrs = rep.ConnectedPeerAddrs
		out.ActiveSubscriptions = rep.ActiveSubscriptions
		out.MessagesPublished = rep.MessagesPublished
		out.MessagesInNetwork = rep.MessagesInNetwork
		out.MessagesInStream = rep.MessagesInStream
		out.MessagesFanout = rep.MessagesFanout
		out.DirectSends = rep.DirectSends
		if out.StartedAt.IsZero() && !rep.StartedAt.IsZero() {
			out.StartedAt = rep.StartedAt
			out.UptimeSec = int64(time.Since(rep.StartedAt).Seconds())
		}
	}

	printStatus(out, *jsonOut)

	if out.Process == "stopped" {
		return exitStopped
	}
	if out.Process == "stale_pid" {
		return exitStopped
	}
	if rpcErr != nil || rep.Error != "" {
		return exitRPCUnhealth
	}
	return 0
}

func runRPCStatus(args []string) int {
	fs := flag.NewFlagSet("rpc status", flag.ContinueOnError)
	configPath := fs.String("config", filepath.Join("data", "assembler.json"), "config file path")
	rpcOverride := fs.String("rpc-sock", "", "rpc socket override")
	jsonOut := fs.Bool("json", false, "print json")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	cfg, err := loadConfig(*configPath)
	if err != nil {
		fmt.Printf("load config failed: %v\n", err)
		return exitRPCUnhealth
	}
	if *rpcOverride != "" {
		cfg.LocalRPCSock = *rpcOverride
	}
	rep, err := rpcGetStatus(cfg.LocalRPCSock)
	if err != nil {
		fmt.Printf("rpc status failed: %v\n", err)
		return exitRPCUnhealth
	}
	if rep.Error != "" {
		fmt.Printf("rpc status error: %s\n", rep.Error)
		return exitRPCUnhealth
	}
	if *jsonOut {
		b, _ := json.MarshalIndent(map[string]any{
			"transport":            rep.Transport,
			"peer_id":              rep.PeerID,
			"connected_peers":      rep.ConnectedPeers,
			"listen_addrs":         rep.ListenAddrs,
			"connected_peer_ids":   rep.ConnectedPeerIDs,
			"active_subscriptions": rep.ActiveSubscriptions,
			"messages_published":   rep.MessagesPublished,
			"messages_in_network":  rep.MessagesInNetwork,
			"messages_in_stream":   rep.MessagesInStream,
			"messages_fanout":      rep.MessagesFanout,
			"direct_sends":         rep.DirectSends,
			"started_at":           rep.StartedAt,
		}, "", "  ")
		fmt.Println(string(b))
	} else {
		fmt.Printf("transport=%s peer_id=%s connected_peers=%d\n", rep.Transport, rep.PeerID, rep.ConnectedPeers)
	}
	return 0
}

func runLogs(args []string) int {
	fs := flag.NewFlagSet("logs", flag.ContinueOnError)
	configPath := fs.String("config", filepath.Join("data", "assembler.json"), "config file path")
	logOverride := fs.String("log-file", "", "log file path override")
	lines := fs.Int("lines", 200, "lines from tail")
	follow := fs.Bool("follow", true, "follow appended logs")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	cfg, err := loadConfig(*configPath)
	if err != nil {
		fmt.Printf("load config failed: %v\n", err)
		return 1
	}
	if *logOverride != "" {
		cfg.RunLogFile = *logOverride
	}
	content, pos, err := tailLines(cfg.RunLogFile, *lines)
	if err != nil {
		fmt.Printf("read logs failed: %v\n", err)
		return 1
	}
	if content != "" {
		fmt.Print(content)
	}
	if !*follow {
		return 0
	}
	for {
		time.Sleep(500 * time.Millisecond)
		f, err := os.Open(cfg.RunLogFile)
		if err != nil {
			continue
		}
		info, err := f.Stat()
		if err != nil {
			_ = f.Close()
			continue
		}
		if info.Size() < pos {
			pos = 0
		}
		if _, err := f.Seek(pos, io.SeekStart); err != nil {
			_ = f.Close()
			continue
		}
		n, _ := io.Copy(os.Stdout, f)
		pos += n
		_ = f.Close()
	}
}

func loadConfig(path string) (runtimeConfig, error) {
	def := runtimeConfig{
		Transport:       "libp2p",
		HTTPAddr:        ":8080",
		P2PListen:       []string{"/ip4/0.0.0.0/tcp/0"},
		P2PBootstrap:    []string{"/ip4/3.65.204.231/tcp/40001/p2p/12D3KooWAaYG182TYGF5GTfWu5CZpiWbf5r6GJwfuSsYRsErA5YL"},
		P2PMDNS:         true,
		P2PRendezvous:   "Assembler",
		P2PIdentityKey:  filepath.Join("data", "p2p_identity.key"),
		P2PRecentPeers:  filepath.Join("data", "recent_peers.json"),
		LocalRPCEnable:  true,
		LocalRPCSock:    filepath.Join("data", "assembler-p2p.sock"),
		LocalRPCRecords: filepath.Join("data", "p2p_messages.jsonl"),
		LocalRPCCursors: filepath.Join("data", "p2p_cursors.json"),
		RunPIDFile:      filepath.Join("data", "run", "assembler.pid"),
		RunLogFile:      filepath.Join("data", "run", "assembler.log"),
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return def, nil
		}
		return runtimeConfig{}, err
	}
	var fc fileConfig
	if err := json.Unmarshal(b, &fc); err != nil {
		return runtimeConfig{}, err
	}
	if fc.Transport != "" {
		def.Transport = fc.Transport
	}
	if fc.HTTPAddr != "" {
		def.HTTPAddr = fc.HTTPAddr
	}
	if len(fc.P2PListen) > 0 {
		def.P2PListen = fc.P2PListen
	}
	if len(fc.P2PBootstrap) > 0 {
		def.P2PBootstrap = fc.P2PBootstrap
	}
	if fc.P2PMDNS != nil {
		def.P2PMDNS = *fc.P2PMDNS
	}
	if fc.P2PRendezvous != "" {
		def.P2PRendezvous = fc.P2PRendezvous
	}
	if fc.P2PIdentityKey != "" {
		def.P2PIdentityKey = fc.P2PIdentityKey
	}
	if fc.P2PRecentPeers != "" {
		def.P2PRecentPeers = fc.P2PRecentPeers
	}
	if fc.LocalRPCEnable != nil {
		def.LocalRPCEnable = *fc.LocalRPCEnable
	}
	if fc.LocalRPCSock != "" {
		def.LocalRPCSock = fc.LocalRPCSock
	}
	if fc.LocalRPCRecords != "" {
		def.LocalRPCRecords = fc.LocalRPCRecords
	}
	if fc.LocalRPCCursors != "" {
		def.LocalRPCCursors = fc.LocalRPCCursors
	}
	if fc.RunPIDFile != "" {
		def.RunPIDFile = fc.RunPIDFile
	}
	if fc.RunLogFile != "" {
		def.RunLogFile = fc.RunLogFile
	}
	return def, nil
}

func buildServerArgs(cfg runtimeConfig) []string {
	args := []string{
		"-addr", cfg.HTTPAddr,
		"-transport", cfg.Transport,
		"-p2p-listen", strings.Join(cfg.P2PListen, ","),
		"-p2p-bootstrap", strings.Join(cfg.P2PBootstrap, ","),
		"-p2p-rendezvous", cfg.P2PRendezvous,
		"-p2p-mdns", strconv.FormatBool(cfg.P2PMDNS),
		"-p2p-identity-key", cfg.P2PIdentityKey,
		"-p2p-recent-peers", cfg.P2PRecentPeers,
		"-local-rpc-enable", strconv.FormatBool(cfg.LocalRPCEnable),
		"-local-rpc-sock", cfg.LocalRPCSock,
		"-local-rpc-records", cfg.LocalRPCRecords,
		"-local-rpc-cursors", cfg.LocalRPCCursors,
	}
	return args
}

func readPID(path string) (int, time.Time, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, time.Time{}, err
	}
	raw := strings.TrimSpace(string(b))
	if raw == "" {
		return 0, time.Time{}, errors.New("empty pid file")
	}
	if strings.HasPrefix(raw, "{") {
		var v struct {
			PID       int       `json:"pid"`
			StartedAt time.Time `json:"started_at"`
		}
		if err := json.Unmarshal(b, &v); err != nil {
			return 0, time.Time{}, err
		}
		if v.PID <= 0 {
			return 0, time.Time{}, errors.New("invalid pid")
		}
		return v.PID, v.StartedAt, nil
	}
	pid, err := strconv.Atoi(raw)
	if err != nil {
		return 0, time.Time{}, err
	}
	return pid, time.Time{}, nil
}

func writePID(path string, pid int, startedAt time.Time) error {
	payload := map[string]any{
		"pid":        pid,
		"started_at": startedAt.Format(time.RFC3339Nano),
	}
	b, _ := json.MarshalIndent(payload, "", "  ")
	return os.WriteFile(path, b, 0o600)
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = p.Signal(syscall.Signal(0))
	return err == nil
}

func ensureDir(path string, mode os.FileMode) error {
	if path == "" || path == "." {
		return nil
	}
	return os.MkdirAll(path, mode)
}

func rpcGetStatus(sock string) (statusReply, error) {
	c, err := rpc.Dial("unix", sock)
	if err != nil {
		return statusReply{}, err
	}
	defer c.Close()
	var reply statusReply
	if err := c.Call("P2P.GetStatus", statusArgs{}, &reply); err != nil {
		return statusReply{}, err
	}
	return reply, nil
}

func printStatus(out statusOutput, asJSON bool) {
	if asJSON {
		b, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(b))
		return
	}
	fmt.Printf("process=%s pid=%d rpc_socket=%s exists=%t\n", out.Process, out.PID, out.RPCSocket, out.RPCSocketExist)
	if !out.StartedAt.IsZero() {
		fmt.Printf("started_at=%s uptime_sec=%d\n", out.StartedAt.Format(time.RFC3339), out.UptimeSec)
	}
	if out.Transport != "" || out.PeerID != "" {
		fmt.Printf("transport=%s peer_id=%s connected_peers=%d\n", out.Transport, out.PeerID, out.ConnectedPeers)
	}
	if len(out.ListenAddrs) > 0 {
		fmt.Printf("listen_addrs=%s\n", strings.Join(out.ListenAddrs, ","))
	}
	fmt.Printf("subs=%d pub=%d net_in=%d stream_in=%d fanout=%d direct_send=%d\n",
		out.ActiveSubscriptions,
		out.MessagesPublished,
		out.MessagesInNetwork,
		out.MessagesInStream,
		out.MessagesFanout,
		out.DirectSends,
	)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func tailLines(path string, lines int) (string, int64, error) {
	if lines <= 0 {
		lines = 200
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", 0, err
	}
	sc := bufio.NewScanner(strings.NewReader(string(b)))
	all := make([]string, 0, lines)
	for sc.Scan() {
		all = append(all, sc.Text())
	}
	if err := sc.Err(); err != nil {
		return "", 0, err
	}
	start := 0
	if len(all) > lines {
		start = len(all) - lines
	}
	chunk := strings.Join(all[start:], "\n")
	if chunk != "" {
		chunk += "\n"
	}
	return chunk, int64(len(b)), nil
}
