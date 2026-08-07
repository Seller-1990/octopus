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
		log.Warnf("sha256sums.txt not available, skipping checksum verification: %v", err)
	} else {
		if err := verifySHA256(data, expectedHash); err != nil {
			log.Warnf("checksum verification failed: %v", err)
			return fmt.Errorf("checksum verification failed: %w", err)
		}
		log.Infof("SHA-256 checksum verified successfully")
	}

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
		} else {
			if err := os.WriteFile(updatedBinPath, data, 0755); err != nil {
				log.Warnf("write binary failed: %v", err)
				return fmt.Errorf("write binary: %w", err)
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
			log.Errorf("restarting failed: %v", err)
		}
		os.Exit(0)
	}

	if err := syscall.Exec(execPath, os.Args, os.Environ()); err != nil {
		log.Errorf("restarting failed: %v", err)
	}
}
