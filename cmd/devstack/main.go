package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go/logging"
	"gopkg.in/yaml.v3"
)

const (
	defaultRoot                = ".devstack"
	relayDir                   = "relay"
	cacheDirName               = ".sbdev"
	defaultServerPort          = 8080
	defaultClientPortStart     = 7938
	defaultMinioAPIPort        = 9000
	defaultMinioConsolePort    = 9001
	defaultBucket              = "syftbox-local"
	defaultRegion              = "us-east-1"
	defaultMinioAdminUser      = "minioadmin"
	defaultMinioAdminPassword  = "minioadmin"
	defaultAccessKey           = "ptSLdKiwOi2LYQFZYEZ6"
	defaultSecretKey           = "GMDvYrAhWDkB2DyFMn8gU8Bg0fT3JGT6iEB7P8"
	serverBuildTags            = "sonic avx nomsgpack"
	clientBuildTags            = "go_json nomsgpack"
	stateFileName              = "state.json"
	minioBinaryName            = "minio"
	minioDownloadBase          = "https://dl.min.io/server/minio/release"
	processShutdownGracePeriod = 8 * time.Second
)

type command string

const (
	cmdStart  command = "start"
	cmdStop   command = "stop"
	cmdStatus command = "status"
	cmdLogs   command = "logs"
	cmdList   command = "list"
	cmdPrune  command = "prune"
)

type stackState struct {
	Root     string         `json:"root"`
	Server   serverState    `json:"server"`
	Minio    minioState     `json:"minio"`
	Clients  []clientState  `json:"clients"`
	Created  time.Time      `json:"created"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type serverState struct {
	PID      int    `json:"pid"`
	Port     int    `json:"port"`
	Config   string `json:"config"`
	LogPath  string `json:"log_path"`
	DataPath string `json:"data_path"`
	BinPath  string `json:"bin_path"`
}

type minioState struct {
	Mode        string `json:"mode"` // local or docker
	PID         int    `json:"pid,omitempty"`
	LogPID      int    `json:"log_pid,omitempty"`
	ContainerID string `json:"container_id,omitempty"`
	APIPort     int    `json:"api_port"`
	ConsolePort int    `json:"console_port"`
	DataPath    string `json:"data_path"`
	LogPath     string `json:"log_path"`
	BinPath     string `json:"bin_path,omitempty"`
}

type clientState struct {
	Email     string `json:"email"`
	PID       int    `json:"pid"`
	Port      int    `json:"port"`
	Config    string `json:"config"`
	DataPath  string `json:"data_path"`
	LogPath   string `json:"log_path"`
	HomePath  string `json:"home_path"`
	BinPath   string `json:"bin_path"`
	ServerURL string `json:"server_url"`
}

type startOptions struct {
	root            string
	clients         []string
	randomPorts     bool
	serverPort      int
	clientPortStart int
	minioAPIPort    int
	minioConsole    int
	useDockerMinio  bool
	keepData        bool
	skipSyncCheck   bool
	reset           bool
}

func addExe(path string) string {
	if runtime.GOOS == "windows" && !strings.HasSuffix(strings.ToLower(path), ".exe") {
		return path + ".exe"
	}
	return path
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: sbdev <start|stop|status|logs|list|prune> [options]")
		os.Exit(1)
	}

	switch command(os.Args[1]) {
	case cmdStart:
		// Auto-prune before starting
		if err := pruneDeadStacks(); err != nil {
			log.Printf("Warning: failed to prune dead stacks: %v", err)
		}
		if err := runStart(os.Args[2:]); err != nil {
			log.Fatalf("start: %v", err)
		}
	case cmdStop:
		if err := runStop(os.Args[2:]); err != nil {
			log.Fatalf("stop: %v", err)
		}
	case cmdStatus:
		if err := runStatus(os.Args[2:]); err != nil {
			log.Fatalf("status: %v", err)
		}
	case cmdLogs:
		if err := runLogs(os.Args[2:]); err != nil {
			log.Fatalf("logs: %v", err)
		}
	case cmdList:
		if err := listActiveStacks(); err != nil {
			log.Fatalf("list: %v", err)
		}
	case cmdPrune:
		if err := pruneDeadStacks(); err != nil {
			log.Fatalf("prune: %v", err)
		}
		fmt.Println("Dead stacks pruned")
	default:
		fmt.Println("usage: sbdev <start|stop|status|logs|list|prune> [options]")
		os.Exit(1)
	}
}

func runStart(args []string) error {
	opts, err := parseStartFlags(args)
	if err != nil {
		return err
	}
	opts.root, err = filepath.Abs(opts.root)
	if err != nil {
		return fmt.Errorf("resolve root: %w", err)
	}

	if len(opts.clients) == 0 {
		return fmt.Errorf("at least one --client email is required")
	}

	if opts.reset {
		stopStack(opts.root) // best effort
		_ = os.RemoveAll(opts.root)
	}

	if err := os.MkdirAll(opts.root, 0o755); err != nil {
		return fmt.Errorf("create root dir: %w", err)
	}

	relayRoot := filepath.Join(opts.root, relayDir)
	if err := os.MkdirAll(relayRoot, 0o755); err != nil {
		return fmt.Errorf("create relay dir: %w", err)
	}

	statePath := filepath.Join(relayRoot, stateFileName)
	oldStatePath := filepath.Join(opts.root, stateFileName)
	if _, err := os.Stat(statePath); err == nil {
		return fmt.Errorf("state already exists at %s – stop it first", statePath)
	}
	if _, err := os.Stat(oldStatePath); err == nil {
		return fmt.Errorf("state already exists at %s – stop it first", oldStatePath)
	}

	binDir := filepath.Join(relayRoot, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return fmt.Errorf("create bin dir: %w", err)
	}

	serverBin := filepath.Join(binDir, "server")
	clientBin := filepath.Join(binDir, "syftbox")

	if err := buildBinary(serverBin, "./cmd/server", serverBuildTags); err != nil {
		return fmt.Errorf("build server: %w", err)
	}
	if err := buildBinary(clientBin, "./cmd/client", clientBuildTags); err != nil {
		return fmt.Errorf("build client: %w", err)
	}

	serverPort := opts.serverPort
	if opts.randomPorts || serverPort == 0 {
		serverPort, err = getFreePort()
		if err != nil {
			return fmt.Errorf("allocate server port: %w", err)
		}
	}

	minioAPIPort := opts.minioAPIPort
	minioConsolePort := opts.minioConsole
	if opts.randomPorts || minioAPIPort == 0 {
		if minioAPIPort, err = getFreePort(); err != nil {
			return fmt.Errorf("allocate minio api port: %w", err)
		}
	}
	if opts.randomPorts || minioConsolePort == 0 {
		if minioConsolePort, err = getFreePort(); err != nil {
			return fmt.Errorf("allocate minio console port: %w", err)
		}
	}

	var clientPortStart = opts.clientPortStart
	if clientPortStart == 0 {
		clientPortStart = defaultClientPortStart
	}

	minioMode := "local"
	minioBin, err := ensureMinioBinary(binDir)
	if err != nil {
		if opts.useDockerMinio || dockerAvailable() {
			minioMode = "docker"
			fmt.Printf("MinIO binary unavailable (%v), falling back to Docker\n", err)
		} else {
			return fmt.Errorf("minio binary missing and docker not requested: %w", err)
		}
	}

	mState, err := startMinio(minioMode, minioBin, relayRoot, minioAPIPort, minioConsolePort, opts.keepData)
	if err != nil {
		return fmt.Errorf("start minio: %w", err)
	}

	serverURL := fmt.Sprintf("http://127.0.0.1:%d", serverPort)
	if err := setupBucket(mState.APIPort); err != nil {
		return fmt.Errorf("minio bootstrap: %w", err)
	}

	sState, err := startServer(serverBin, relayRoot, serverPort, mState.APIPort)
	if err != nil {
		stopMinio(mState) // best effort cleanup
		return fmt.Errorf("start server: %w", err)
	}

	var clients []clientState
	for i, email := range opts.clients {
		port := clientPortStart + i
		if opts.randomPorts {
			port, err = getFreePort()
			if err != nil {
				return fmt.Errorf("allocate client port for %s: %w", email, err)
			}
		}

		cState, err := startClient(clientBin, opts.root, email, serverURL, port)
		if err != nil {
			return fmt.Errorf("start client %s: %w", email, err)
		}
		clients = append(clients, cState)
	}

	state := stackState{
		Root:    opts.root,
		Server:  sState,
		Minio:   mState,
		Clients: clients,
		Created: time.Now().UTC(),
	}

	// Save to global state directory
	if err := saveGlobalState(opts.root, &state); err != nil {
		return fmt.Errorf("save global state: %w", err)
	}

	// Also save locally for backward compatibility
	if err := writeState(statePath, &state); err != nil {
		log.Printf("Warning: failed to write local state: %v", err)
	}

	fmt.Printf("Devstack started in %s\n", opts.root)
	fmt.Printf("  Server: %s (pid %d)\n", serverURL, sState.PID)
	fmt.Printf("  MinIO:  http://127.0.0.1:%d (console http://127.0.0.1:%d)\n", mState.APIPort, mState.ConsolePort)
	for _, c := range clients {
		fmt.Printf("  Client: %s (daemon http://127.0.0.1:%d pid %d)\n", c.Email, c.Port, c.PID)
	}
	fmt.Printf("State: %s\n", statePath)

	if !opts.skipSyncCheck {
		if err := runSyncCheck(opts.root, opts.clients); err != nil {
			fmt.Printf("Sync check warning (continuing): %v\n", err)
		}
	}
	return nil
}

func parseStartFlags(args []string) (startOptions, error) {
	opts := startOptions{
		root:            defaultRoot,
		serverPort:      defaultServerPort,
		clientPortStart: defaultClientPortStart,
		minioAPIPort:    defaultMinioAPIPort,
		minioConsole:    defaultMinioConsolePort,
	}

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--path":
			i++
			opts.root = args[i]
		case "--client":
			i++
			opts.clients = append(opts.clients, args[i])
		case "--random-ports":
			opts.randomPorts = true
		case "--server-port":
			i++
			opts.serverPort = atoi(args[i])
		case "--client-port-start":
			i++
			opts.clientPortStart = atoi(args[i])
		case "--minio-api-port":
			i++
			opts.minioAPIPort = atoi(args[i])
		case "--minio-console-port":
			i++
			opts.minioConsole = atoi(args[i])
		case "--docker-minio":
			opts.useDockerMinio = true
		case "--keep-data":
			opts.keepData = true
		case "--skip-sync-check":
			opts.skipSyncCheck = true
		case "--reset":
			opts.reset = true
		default:
			return opts, fmt.Errorf("unknown flag %s", args[i])
		}
	}

	return opts, nil
}

func buildBinary(outPath, pkg, tags string) error {
	// Force rebuild all packages to ensure latest code changes are included
	args := []string{"build", "-a", "-tags", tags, "-o", outPath, pkg}
	cmd := exec.Command("go", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func ensureMinioBinary(binDir string) (string, error) {
	if path, err := exec.LookPath(minioBinaryName); err == nil {
		return path, nil
	}

	// check global cache
	cachePath := filepath.Join(os.Getenv("HOME"), cacheDirName, "bin", minioBinaryName)
	if _, err := os.Stat(cachePath); err == nil {
		return cachePath, nil
	}

	target := filepath.Join(binDir, minioBinaryName)
	if _, err := os.Stat(target); err == nil {
		return target, nil
	}

	if err := downloadMinio(target); err != nil {
		return "", err
	}
	// also copy to cache for future runs
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err == nil {
		_ = copyFile(target, cachePath)
		_ = os.Chmod(cachePath, 0o755)
	}
	return target, nil
}

func dockerAvailable() bool {
	_, err := exec.LookPath("docker")
	return err == nil
}

func downloadMinio(dest string) error {
	osName := runtime.GOOS
	arch := runtime.GOARCH
	var platform string
	switch osName {
	case "darwin":
		if arch == "arm64" {
			platform = "darwin-arm64"
		} else {
			platform = "darwin-amd64"
		}
	case "linux":
		if arch == "arm64" {
			platform = "linux-arm64"
		} else {
			platform = "linux-amd64"
		}
	default:
		return fmt.Errorf("unsupported platform for minio download: %s/%s", osName, arch)
	}

	url := fmt.Sprintf("%s/%s/%s", minioDownloadBase, platform, minioBinaryName)
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(url) //nolint:gosec
	if err != nil {
		return fmt.Errorf("download minio: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download minio: unexpected status %s", resp.Status)
	}

	tmp := dest + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := ioCopy(out, resp.Body); err != nil {
		out.Close()
		return err
	}
	out.Close()

	if err := os.Chmod(tmp, 0o755); err != nil {
		return err
	}
	return os.Rename(tmp, dest)
}

func stopStack(root string) error {
	state, statePath, err := readState(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}

	for _, c := range state.Clients {
		_ = killProcess(c.PID)
	}
	if state.Server.PID > 0 {
		_ = killProcess(state.Server.PID)
	}
	stopMinio(state.Minio)

	if err := os.Remove(statePath); err != nil {
		return fmt.Errorf("remove state: %w", err)
	}
	fmt.Printf("Stopped stack at %s\n", root)
	return nil
}

func startMinio(mode, binPath, root string, apiPort, consolePort int, keepData bool) (minioState, error) {
	if mode == "docker" {
		return startMinioDocker(root, apiPort, consolePort)
	}

	dataDir := filepath.Join(root, "minio", "data")
	logDir := filepath.Join(root, "minio", "logs")
	if !keepData {
		_ = os.RemoveAll(dataDir)
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return minioState{}, err
	}
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return minioState{}, err
	}

	logFile := filepath.Join(logDir, "minio.log")
	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return minioState{}, err
	}

	addr := fmt.Sprintf(":%d", apiPort)
	console := fmt.Sprintf(":%d", consolePort)

	cmd := exec.Command(binPath, "server", dataDir, "--address", addr, "--console-address", console)
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("MINIO_ROOT_USER=%s", defaultMinioAdminUser),
		fmt.Sprintf("MINIO_ROOT_PASSWORD=%s", defaultMinioAdminPassword),
	)
	cmd.Stdout = f
	cmd.Stderr = f

	if err := cmd.Start(); err != nil {
		return minioState{}, err
	}

	if err := waitForMinio(apiPort); err != nil {
		return minioState{}, fmt.Errorf("minio health: %w", err)
	}

	return minioState{
		Mode:        "local",
		PID:         cmd.Process.Pid,
		APIPort:     apiPort,
		ConsolePort: consolePort,
		DataPath:    dataDir,
		LogPath:     logFile,
		BinPath:     binPath,
	}, nil
}

func startMinioDocker(root string, apiPort, consolePort int) (minioState, error) {
	dataDir := filepath.Join(root, "minio", "data")
	logDir := filepath.Join(root, "minio", "logs")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return minioState{}, err
	}
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return minioState{}, err
	}

	containerName := fmt.Sprintf("devstack-minio-%d", time.Now().Unix())
	args := []string{
		"run", "-d",
		"--name", containerName,
		"-p", fmt.Sprintf("%d:9000", apiPort),
		"-p", fmt.Sprintf("%d:9001", consolePort),
		"-e", fmt.Sprintf("MINIO_ROOT_USER=%s", defaultMinioAdminUser),
		"-e", fmt.Sprintf("MINIO_ROOT_PASSWORD=%s", defaultMinioAdminPassword),
		"-v", fmt.Sprintf("%s:/data", dataDir),
		"minio/minio:RELEASE.2025-04-22T22-12-26Z",
		"server", "/data", "--console-address", ":9001",
	}
	cmd := exec.Command("docker", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return minioState{}, fmt.Errorf("docker run: %w (%s)", err, string(out))
	}

	if err := waitForMinio(apiPort); err != nil {
		return minioState{}, fmt.Errorf("minio health: %w", err)
	}

	logFile := filepath.Join(logDir, "minio.log")
	lf, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return minioState{}, err
	}
	logCmd := exec.Command("docker", "logs", "-f", containerName)
	logCmd.Stdout = lf
	logCmd.Stderr = lf
	if err := logCmd.Start(); err != nil {
		return minioState{}, fmt.Errorf("docker logs: %w", err)
	}

	return minioState{
		Mode:        "docker",
		ContainerID: containerName,
		APIPort:     apiPort,
		ConsolePort: consolePort,
		DataPath:    dataDir,
		LogPath:     logFile,
		LogPID:      logCmd.Process.Pid,
	}, nil
}

func waitForMinio(apiPort int) error {
	url := fmt.Sprintf("http://127.0.0.1:%d/minio/health/live", apiPort)
	for i := 0; i < 40; i++ {
		resp, err := http.Get(url) //nolint:gosec
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("minio did not become healthy")
}

func setupBucket(apiPort int) error {
	endpoint := fmt.Sprintf("http://127.0.0.1:%d", apiPort)
	cfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(defaultMinioAdminUser, defaultMinioAdminPassword, ""),
		),
		config.WithRegion(defaultRegion),
		config.WithLogger(logging.Nop{}),
	)
	if err != nil {
		return err
	}

	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
		o.UsePathStyle = true
	})

	_, err = client.CreateBucket(context.Background(), &s3.CreateBucketInput{
		Bucket: aws.String(defaultBucket),
	})
	if err != nil && !isBucketExistsError(err) {
		return err
	}
	return nil
}

func isBucketExistsError(err error) bool {
	var owned *s3types.BucketAlreadyOwnedByYou
	var exists *s3types.BucketAlreadyExists
	return errors.As(err, &owned) || errors.As(err, &exists)
}

func startServer(binPath, relayRoot string, port, minioPort int) (serverState, error) {
	serverDir := filepath.Join(relayRoot, "server")
	logDir := filepath.Join(serverDir, "logs")
	dataDir := filepath.Join(serverDir, "data")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return serverState{}, err
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return serverState{}, err
	}
	configPath := filepath.Join(serverDir, "config.yaml")
	if err := writeServerConfig(configPath, port, minioPort, dataDir, logDir); err != nil {
		return serverState{}, err
	}

	logFile := filepath.Join(logDir, "server.log")
	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return serverState{}, err
	}

	cmd := exec.Command(binPath, "--config", configPath)
	cmd.Stdout = f
	cmd.Stderr = f
	cmd.Dir = relayRoot

	if err := cmd.Start(); err != nil {
		return serverState{}, err
	}

	return serverState{
		PID:      cmd.Process.Pid,
		Port:     port,
		Config:   configPath,
		LogPath:  logFile,
		DataPath: dataDir,
		BinPath:  binPath,
	}, nil
}

func stopMinio(ms minioState) {
	if ms.Mode == "docker" && ms.ContainerID != "" {
		_ = exec.Command("docker", "rm", "-f", ms.ContainerID).Run()
		if ms.LogPID > 0 {
			_ = killProcess(ms.LogPID)
		}
		return
	}
	if ms.PID > 0 {
		_ = killProcess(ms.PID)
	}
}

func writeServerConfig(path string, port, minioPort int, dataDir, logDir string) error {
	cfg := map[string]any{
		"http": map[string]any{
			"addr": fmt.Sprintf("127.0.0.1:%d", port),
		},
		"blob": map[string]any{
			"bucket_name": defaultBucket,
			"region":      defaultRegion,
			"endpoint":    fmt.Sprintf("http://127.0.0.1:%d", minioPort),
			"access_key":  defaultMinioAdminUser,
			"secret_key":  defaultMinioAdminPassword,
		},
		"auth": map[string]any{
			"enabled": false,
		},
		"email": map[string]any{
			"enabled": false,
		},
		"data_dir": dataDir,
		"log_dir":  logDir,
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func startClient(binPath, root, email, serverURL string, port int) (clientState, error) {
	emailDir := filepath.Join(root, email)
	homeDir := emailDir
	configDir := filepath.Join(homeDir, ".syftbox")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return clientState{}, err
	}
	dataDir := emailDir
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return clientState{}, err
	}
	logDir := filepath.Join(homeDir, ".syftbox", "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return clientState{}, err
	}

	clientURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	configPath := filepath.Join(configDir, "config.json")
	cfg := map[string]any{
		"data_dir":     dataDir,
		"email":        email,
		"server_url":   serverURL,
		"client_url":   clientURL,
		"client_token": "",
	}
	cfgData, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return clientState{}, err
	}
	if err := os.WriteFile(configPath, cfgData, 0o644); err != nil {
		return clientState{}, err
	}

	logFile := filepath.Join(emailDir, "client-daemon.log")
	lf, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return clientState{}, err
	}

	cmd := exec.Command(binPath, "-c", configPath, "daemon", "--http-addr", fmt.Sprintf("127.0.0.1:%d", port))
	cmd.Stdout = lf
	cmd.Stderr = lf
	cmd.Env = append(os.Environ(),
		"HOME="+homeDir,
		"SYFTBOX_CONFIG_PATH="+configPath,
		"SYFTBOX_DATA_DIR="+dataDir,
		"SYFTBOX_SERVER_URL="+serverURL,
	)

	if err := cmd.Start(); err != nil {
		return clientState{}, err
	}

	return clientState{
		Email:     email,
		PID:       cmd.Process.Pid,
		Port:      port,
		Config:    configPath,
		DataPath:  dataDir,
		LogPath:   logFile,
		HomePath:  homeDir,
		BinPath:   binPath,
		ServerURL: serverURL,
	}, nil
}

func runSyncCheck(root string, emails []string) error {
	if len(emails) <= 1 {
		return nil
	}

	src := emails[0]
	filename := fmt.Sprintf("devstack-ready-%d.txt", time.Now().UnixNano())
	content := fmt.Sprintf("devstack ready %s", time.Now().Format(time.RFC3339Nano))

	publicDir := publicPath(root, src)
	if err := os.MkdirAll(publicDir, 0o755); err != nil {
		return fmt.Errorf("ensure source public dir: %w", err)
	}
	if err := waitForDir(publicDir, 15*time.Second); err != nil {
		fmt.Printf("Sync probe note: source public dir not ready (%s): %v\n", publicDir, err)
		return nil
	}

	filePath := filepath.Join(publicDir, filename)
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		fmt.Printf("Sync probe note: write failed: %v\n", err)
		return nil
	}

	// Touch the file via the server to trigger notifications
	if err := waitForServerReady(root, 15*time.Second); err != nil {
		return fmt.Errorf("wait for server: %w", err)
	}
	time.Sleep(500 * time.Millisecond)
	if err := triggerDownloadForAll(root, emails, filename); err != nil {
		fmt.Printf("Sync probe trigger note: %v (continuing)\n", err)
	}

	for _, email := range emails[1:] {
		// Each client syncs alice's public dir to their local <root>/<client>/<sender>/public
		targetDir := filepath.Join(root, email, "datasites", src, "public")
		_ = os.MkdirAll(targetDir, 0o755) // best-effort
		target := filepath.Join(targetDir, filename)
		if err := waitForFile(target, content, 45*time.Second); err != nil {
			fmt.Printf("Sync probe note: %s did not see probe yet (%v)\n", email, err)
			return nil
		}
	}

	fmt.Printf("Sync check passed (%s replicated to %d clients)\n", filename, len(emails)-1)
	return nil
}

func publicPath(root, email string) string {
	// Workspace root is <root>/<email>; actual public dir lives under <root>/<email>/datasites/<email>/public
	return filepath.Join(root, email, "datasites", email, "public")
}

// triggerDownloadForAll asks the server to serve the probe for each user to ensure their daemon pulls it.
func triggerDownloadForAll(root string, emails []string, filename string) error {
	state, _, err := readState(root)
	if err != nil {
		return err
	}
	serverURL := fmt.Sprintf("http://127.0.0.1:%d", state.Server.Port)
	client := &http.Client{Timeout: 3 * time.Second}
	src := emails[0] // probe file created by first client
	for _, email := range emails {
		payload := fmt.Sprintf(`{"keys":["%s/public/%s"]}`, src, filename)
		url := fmt.Sprintf("%s/api/v1/blob/download?user=%s", serverURL, email)
		if err := postWithRetry(client, url, payload, 30, 500*time.Millisecond); err != nil {
			return fmt.Errorf("trigger for %s: %w", email, err)
		}
	}
	return nil
}

func waitForDir(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("dir not found: %s", path)
}

func waitForFile(path, want string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil && string(data) == want {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("file not found or mismatched: %s", path)
}

func waitForServerReady(root string, timeout time.Duration) error {
	state, _, err := readState(root)
	if err != nil {
		return err
	}
	url := fmt.Sprintf("http://127.0.0.1:%d/healthz", state.Server.Port)
	return getWithRetry(url, timeout)
}

func runStop(args []string) error {
	root := defaultRoot
	for i := 0; i < len(args); i++ {
		if args[i] == "--path" {
			i++
			root = args[i]
		}
	}
	var err error
	root, err = filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("resolve root: %w", err)
	}
	return stopStack(root)
}

func runStatus(args []string) error {
	root := defaultRoot
	for i := 0; i < len(args); i++ {
		if args[i] == "--path" {
			i++
			root = args[i]
		}
	}
	var err error
	root, err = filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("resolve root: %w", err)
	}
	state, _, err := readState(root)
	if err != nil {
		return err
	}

	fmt.Printf("Stack at %s (created %s)\n", state.Root, state.Created.Format(time.RFC3339))
	fmt.Printf("  Server: pid %d port %d log %s\n", state.Server.PID, state.Server.Port, state.Server.LogPath)
	fmt.Printf("  MinIO:  %s api %d console %d log %s\n", state.Minio.Mode, state.Minio.APIPort, state.Minio.ConsolePort, state.Minio.LogPath)
	for _, c := range state.Clients {
		fmt.Printf("  Client: %s pid %d port %d log %s\n", c.Email, c.PID, c.Port, c.LogPath)
	}
	return nil
}

func runLogs(args []string) error {
	root := defaultRoot
	for i := 0; i < len(args); i++ {
		if args[i] == "--path" {
			i++
			root = args[i]
		}
	}
	var err error
	root, err = filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("resolve root: %w", err)
	}
	state, _, err := readState(root)
	if err != nil {
		return err
	}
	fmt.Println("Log locations:")
	fmt.Printf("  Server: %s\n", state.Server.LogPath)
	fmt.Printf("  MinIO:  %s\n", state.Minio.LogPath)
	for _, c := range state.Clients {
		fmt.Printf("  Client %s: %s and %s\n", c.Email, c.LogPath, filepath.Join(c.HomePath, ".syftbox", "logs", "syftbox.log"))
	}
	return nil
}

func writeState(path string, state *stackState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func readState(root string) (*stackState, string, error) {
	path := statePathForRoot(root)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, "", err
		}
		return nil, "", fmt.Errorf("read state: %w", err)
	}
	var state stackState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, "", fmt.Errorf("decode state: %w", err)
	}
	return &state, path, nil
}

func statePathForRoot(root string) string {
	// Try global state directory first
	globalPath, err := getGlobalStatePath(root)
	if err == nil {
		if _, err := os.Stat(globalPath); err == nil {
			return globalPath
		}
	}

	// Fall back to local state
	relayRoot := filepath.Join(root, relayDir)
	newPath := filepath.Join(relayRoot, stateFileName)
	if _, err := os.Stat(newPath); err == nil {
		return newPath
	}
	// backward compatibility
	return filepath.Join(root, stateFileName)
}

// getGlobalStateDir returns ~/.sbdev directory
func getGlobalStateDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home dir: %w", err)
	}
	return filepath.Join(home, ".sbdev"), nil
}

// getStackID creates a unique ID for a stack root path
func getStackID(root string) string {
	absRoot, _ := filepath.Abs(root)
	hash := sha256.Sum256([]byte(absRoot))
	return fmt.Sprintf("%x", hash[:8]) // Use first 8 bytes
}

// getGlobalStatePath returns the global state file path for a stack
func getGlobalStatePath(root string) (string, error) {
	globalDir, err := getGlobalStateDir()
	if err != nil {
		return "", err
	}
	stackID := getStackID(root)
	return filepath.Join(globalDir, "stacks", stackID, stateFileName), nil
}

// saveGlobalState saves state to global directory
func saveGlobalState(root string, state *stackState) error {
	globalPath, err := getGlobalStatePath(root)
	if err != nil {
		return err
	}

	// Create stack directory
	stackDir := filepath.Dir(globalPath)
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		return fmt.Errorf("create stack dir: %w", err)
	}

	// Save absolute path for reference
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	pathFile := filepath.Join(stackDir, "path.txt")
	if err := os.WriteFile(pathFile, []byte(absRoot), 0o644); err != nil {
		return fmt.Errorf("write path file: %w", err)
	}

	// Write state
	return writeState(globalPath, state)
}

// pruneDeadStacks removes stacks with dead processes
func pruneDeadStacks() error {
	globalDir, err := getGlobalStateDir()
	if err != nil {
		return err
	}

	stacksDir := filepath.Join(globalDir, "stacks")
	entries, err := os.ReadDir(stacksDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		stackDir := filepath.Join(stacksDir, entry.Name())
		statePath := filepath.Join(stackDir, stateFileName)

		// Read state
		data, err := os.ReadFile(statePath)
		if err != nil {
			continue
		}

		var state stackState
		if err := json.Unmarshal(data, &state); err != nil {
			continue
		}

		// Check if any processes are alive
		allDead := true
		if processExists(state.Server.PID) {
			allDead = false
		}
		if state.Minio.PID > 0 && processExists(state.Minio.PID) {
			allDead = false
		}
		for _, client := range state.Clients {
			if processExists(client.PID) {
				allDead = false
				break
			}
		}

		// Remove if all processes are dead
		if allDead {
			log.Printf("Pruning dead stack: %s (from %s)", entry.Name(), state.Root)
			if err := os.RemoveAll(stackDir); err != nil {
				log.Printf("Failed to remove %s: %v", stackDir, err)
			}
		}
	}

	return nil
}

// listActiveStacks shows all tracked stacks
func listActiveStacks() error {
	globalDir, err := getGlobalStateDir()
	if err != nil {
		return err
	}

	stacksDir := filepath.Join(globalDir, "stacks")
	entries, err := os.ReadDir(stacksDir)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("No active stacks")
			return nil
		}
		return err
	}

	fmt.Printf("Active devstacks in %s:\n\n", stacksDir)

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		stackDir := filepath.Join(stacksDir, entry.Name())
		statePath := filepath.Join(stackDir, stateFileName)
		pathFile := filepath.Join(stackDir, "path.txt")

		// Read path
		pathData, err := os.ReadFile(pathFile)
		if err != nil {
			continue
		}
		stackPath := strings.TrimSpace(string(pathData))

		// Read state
		data, err := os.ReadFile(statePath)
		if err != nil {
			continue
		}

		var state stackState
		if err := json.Unmarshal(data, &state); err != nil {
			continue
		}

		// Check process status
		serverAlive := processExists(state.Server.PID)
		minioAlive := state.Minio.PID > 0 && processExists(state.Minio.PID)
		clientsAlive := 0
		for _, client := range state.Clients {
			if processExists(client.PID) {
				clientsAlive++
			}
		}

		status := "🟢"
		if !serverAlive || !minioAlive || clientsAlive != len(state.Clients) {
			status = "🔴"
		}

		fmt.Printf("%s %s\n", status, stackPath)
		fmt.Printf("   ID: %s\n", entry.Name())
		fmt.Printf("   Server: %d (port %d) - alive: %v\n", state.Server.PID, state.Server.Port, serverAlive)
		fmt.Printf("   MinIO: %d (port %d) - alive: %v\n", state.Minio.PID, state.Minio.APIPort, minioAlive)
		fmt.Printf("   Clients: %d alive / %d total\n", clientsAlive, len(state.Clients))
		fmt.Println()
	}

	return nil
}

// processExists checks if a process is running
func processExists(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = process.Signal(syscall.Signal(0))
	return err == nil
}

func killProcess(pid int) error {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	_ = proc.Signal(syscall.SIGTERM)
	done := make(chan struct{})
	go func() {
		proc.Wait() //nolint:errcheck
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(processShutdownGracePeriod):
		_ = proc.Kill()
	}
	return nil
}

func getFreePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

func atoi(s string) int {
	v, _ := strconv.Atoi(s)
	return v
}

func ioCopy(dst *os.File, src io.Reader) (int64, error) {
	return io.Copy(dst, src)
}

func postWithRetry(client *http.Client, url, payload string, attempts int, backoff time.Duration) error {
	for i := 0; i < attempts; i++ {
		req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(payload))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err == nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return nil
			}
			err = fmt.Errorf("status %d", resp.StatusCode)
		}
		if i == attempts-1 {
			return err
		}
		time.Sleep(backoff)
	}
	return fmt.Errorf("exhausted retries for %s", url)
}

func getWithRetry(url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url) //nolint:gosec,noctx
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			return nil
		}
		if resp != nil && resp.Body != nil {
			resp.Body.Close()
		}
		time.Sleep(300 * time.Millisecond)
	}
	return fmt.Errorf("server not ready at %s", url)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}
