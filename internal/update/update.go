package update

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"

	"github.com/bestruirui/octopus/internal/conf"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/bestruirui/octopus/internal/utils/shutdown"
)

var (
	updateMu   sync.Mutex
	restarting atomic.Bool
)

func UpdateCore() error {
	if restarting.Load() {
		return fmt.Errorf("update completed, server is restarting")
	}
	if !updateMu.TryLock() {
		return fmt.Errorf("update already in progress")
	}
	defer updateMu.Unlock()

	log.Infof("start update core")
	if !AutoUpdateSupported() {
		err := fmt.Errorf("auto update is disabled on %s; install the latest release from %s/releases/latest", runtime.GOOS, strings.TrimRight(conf.Repo, "/"))
		log.Warnf("update core failed: %v", err)
		return err
	}

	filename, err := getDownloadFilename()
	if err != nil {
		log.Warnf("update core failed: %v", err)
		return err
	}

	downloadUrl := releaseDownloadURL(filename)
	log.Infof("download url: %s", downloadUrl)
	data, err := doRequestWithFallback(downloadUrl)
	if err != nil {
		log.Warnf("download failed: %v", err)
		return err
	}

	if len(data) == 0 {
		return fmt.Errorf("downloaded binary is empty (0 bytes)")
	}

	// SHA-256 checksum verification
	checksumURL := releaseDownloadURL("sha256sums.txt")
	expectedHash, err := fetchExpectedChecksum(checksumURL, filename)
	if err != nil {
		// 校验源不可用即拒绝更新：热更新是替换自身二进制的不可逆动作，
		// 未校验的下载体（截断/污染）一旦写入并重启会直接挂掉服务。
		// 更新失败可以随时重试，坏二进制无法自愈。
		log.Warnf("sha256sums.txt unavailable, aborting update (never install unverified binaries): %v", err)
		return fmt.Errorf("checksum verification unavailable: %w", err)
	}
	if err := verifySHA256(data, expectedHash); err != nil {
		log.Warnf("checksum verification failed: %v", err)
		return fmt.Errorf("checksum verification failed: %w", err)
	}
	log.Infof("SHA-256 checksum verified successfully")

	// Determine target path based on environment
	var targetDir string
	var updatedBinPath string

	if InContainer() {
		targetDir = ContainerDataDir()
		if err := os.MkdirAll(targetDir, 0755); err != nil {
			log.Warnf("failed to create data dir: %v", err)
			return fmt.Errorf("create data dir: %w", err)
		}
		updatedBinPath = filepath.Join(targetDir, "octopus-updated")
		if err := os.Remove(updatedBinPath); err != nil && !os.IsNotExist(err) {
			log.Warnf("failed to remove old updated binary, aborting: %v", err)
			return fmt.Errorf("remove old updated binary: %w", err)
		}
		log.Infof("container mode: updating binary to %s", updatedBinPath)
	} else {
		execPath, err := os.Executable()
		if err != nil {
			log.Warnf("get executable path failed: %v", err)
			return err
		}
		execPath, err = filepath.EvalSymlinks(execPath)
		if err != nil {
			log.Warnf("resolve symlink failed: %v", err)
			return err
		}
		targetDir = filepath.Dir(execPath)
		updatedBinPath = execPath

		backupPath := execPath + ".backup"
		os.Remove(backupPath)
		if err := os.Rename(execPath, backupPath); err != nil {
			log.Warnf("rename failed, falling back to copy: %v", err)
			if err := copyFile(execPath, backupPath); err != nil {
				log.Warnf("failed to backup current binary, aborting update: %v", err)
				return fmt.Errorf("backup current binary failed: %w", err)
			}
			os.Remove(execPath)
		}

		// Deferred rollback: if write fails, restore backup
		writeSuccess := false
		defer func() {
			if !writeSuccess {
				log.Warnf("update write failed, rolling back from backup")
				if rbErr := os.Rename(backupPath, execPath); rbErr != nil {
					log.Errorf("rollback failed: %v (backup at %s)", rbErr, backupPath)
				}
			}
		}()

		isZip := strings.HasSuffix(filename, ".zip")
		if isZip {
			if err := unzip(data, targetDir); err != nil {
				log.Warnf("unzip failed: %v", err)
				return err
			}
			// B250913-15：unzip 成功不代表包内二进制真实解出（空 zip/路径不符），
			// 且 zip 内条目不保证可执行位——验证+chmod 后才算写入成功。
			if err := verifyUpdatedBinary(updatedBinPath); err != nil {
				return err
			}
		} else {
			if err := os.WriteFile(updatedBinPath, data, 0755); err != nil {
				log.Warnf("write binary failed: %v", err)
				return fmt.Errorf("write binary: %w", err)
			}
			if err := verifyUpdatedBinary(updatedBinPath); err != nil {
				return err
			}
		}
		writeSuccess = true
		log.Infof("update core success")
		restarting.Store(true)
		go restartExecutable(updatedBinPath)
		return nil
	}

	// Container path: write
	isZip := strings.HasSuffix(filename, ".zip")
	if isZip {
		if err := unzip(data, targetDir); err != nil {
			log.Warnf("unzip failed: %v", err)
			return err
		}
		if err := verifyUpdatedBinary(updatedBinPath); err != nil {
			return err
		}
		unzippedPath := filepath.Join(targetDir, "octopus")
		if _, err := os.Stat(unzippedPath); err == nil {
			os.Chmod(unzippedPath, 0755)
			if unzippedPath != updatedBinPath {
				if err := os.Rename(unzippedPath, updatedBinPath); err != nil {
					return fmt.Errorf("rename updated binary: %w", err)
				}
			}
		}
	} else {
		if err := os.WriteFile(updatedBinPath, data, 0755); err != nil {
			log.Warnf("write binary failed: %v", err)
			return fmt.Errorf("write binary: %w", err)
		}
	}

	log.Infof("update core success")
	restarting.Store(true)
	go restartExecutable(updatedBinPath)
	return nil
}

// verifySHA256 checks that the SHA-256 hash of data matches expectedHash.
func verifySHA256(data []byte, expectedHash string) error {
	actual := sha256.Sum256(data)
	actualHex := hex.EncodeToString(actual[:])
	if !strings.EqualFold(actualHex, strings.TrimSpace(expectedHash)) {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedHash, actualHex)
	}
	return nil
}

// fetchExpectedChecksum downloads sha256sums.txt and returns the hash for filename.
func fetchExpectedChecksum(checksumURL string, filename string) (string, error) {
	data, err := doRequestWithFallback(checksumURL)
	if err != nil {
		return "", fmt.Errorf("download sha256sums.txt: %w", err)
	}

	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 2 && parts[1] == filename {
			return parts[0], nil
		}
	}
	return "", fmt.Errorf("checksum for %s not found in sha256sums.txt", filename)
}

// copyFile copies src to dst for backup purposes.
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
	_, err = io.Copy(out, in)
	return err
}

func AutoUpdateSupported() bool {
	return autoUpdateSupported(runtime.GOOS)
}

func autoUpdateSupported(goos string) bool {
	return goos != "windows"
}

func getDownloadFilename() (string, error) {
	arch := runtime.GOARCH
	goos := runtime.GOOS

	switch goos {
	case "windows":
		switch arch {
		case "386":
			return "octopus-windows-x86.zip", nil
		case "amd64":
			return "octopus-windows-x86_64.zip", nil
		}
	case "darwin":
		switch arch {
		case "amd64":
			return "octopus-darwin-x86_64.zip", nil
		case "arm64":
			return "octopus-darwin-arm64.zip", nil
		}
	case "linux":
		switch arch {
		case "386":
			return "octopus-linux-x86", nil
		case "amd64":
			return "octopus-linux-x86_64", nil
		case "arm":
			return "octopus-linux-armv7", nil
		case "arm64":
			return "octopus-linux-arm64", nil
		}
	}
	return "", fmt.Errorf("unsupported platform: %s/%s", goos, arch)
}

func restartExecutable(execPath string) {
	shutdown.Shutdown()

	log.Infof("restarting: %q %q", execPath, os.Args[1:])

	if runtime.GOOS == "windows" {
		cmd := exec.Command(execPath, os.Args[1:]...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			// B250913-15：启动失败若只打日志，旧进程继续跑新标志位
			// （restarting=true 永久拒绝后续更新）且无进程接管。回滚备份后
			// 退出，交给进程管理器用旧版本拉起。
			log.Errorf("restarting failed: %v (rolling back and exiting)", err)
			rollbackUpdatedBinary(execPath)
			os.Exit(1)
		}
		os.Exit(0)
	}

	if err := syscall.Exec(execPath, os.Args, os.Environ()); err != nil {
		log.Errorf("restarting failed: %v (rolling back and exiting)", err)
		rollbackUpdatedBinary(execPath)
		os.Exit(1)
	}
}

// verifyUpdatedBinary 确认目标二进制存在、是常规文件且非空，并补可执行位。
func verifyUpdatedBinary(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("updated binary missing at %s: %w", path, err)
	}
	if info.IsDir() {
		return fmt.Errorf("updated binary path is a directory: %s", path)
	}
	if info.Size() == 0 {
		return fmt.Errorf("updated binary is empty: %s", path)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(path, 0755); err != nil {
			return fmt.Errorf("chmod updated binary: %w", err)
		}
	}
	return nil
}

// rollbackUpdatedBinary 用 .backup 恢复旧二进制（尽力而为；容器路径无备份）。
func rollbackUpdatedBinary(execPath string) {
	backupPath := execPath + ".backup"
	if _, err := os.Stat(backupPath); err != nil {
		log.Warnf("no backup to roll back at %s", backupPath)
		return
	}
	if err := os.Rename(backupPath, execPath); err != nil {
		log.Errorf("rollback failed: %v (backup at %s)", err, backupPath)
		return
	}
	log.Warnf("rolled back updated binary from %s", backupPath)
}
