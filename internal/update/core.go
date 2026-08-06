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
	"syscall"

	"github.com/bestruirui/octopus/internal/conf"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/bestruirui/octopus/internal/utils/shutdown"
)

func UpdateCore() error {
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
		// In container: write updated binary to data volume so it persists across restarts
		targetDir = ContainerDataDir()
		if err := os.MkdirAll(targetDir, 0755); err != nil {
			log.Warnf("failed to create data dir: %v", err)
			return fmt.Errorf("create data dir: %w", err)
		}
		updatedBinPath = filepath.Join(targetDir, "octopus-updated")
		log.Infof("container mode: updating binary to %s", updatedBinPath)
	} else {
		// Bare metal: overwrite in place
		execPath, err := os.Executable()
		if err != nil {
			log.Warnf("get executable path failed: %v", err)
			return err
		}
		targetDir = filepath.Dir(execPath)
		updatedBinPath = execPath

		// Backup current binary before replacement
		backupPath := execPath + ".backup"
		os.Remove(backupPath)
		if err := copyFile(execPath, backupPath); err != nil {
			log.Warnf("failed to backup current binary: %v", err)
		}
	}

	if err := unzip(data, targetDir); err != nil {
		log.Warnf("unzip failed: %v", err)
		return err
	}

	// In container mode, the unzipped binary is named "octopus"; rename it.
	if InContainer() {
		unzippedPath := filepath.Join(targetDir, "octopus")
		if _, err := os.Stat(unzippedPath); err == nil {
			if err := os.Chmod(unzippedPath, 0755); err != nil {
				log.Warnf("chmod failed: %v", err)
			}
			if unzippedPath != updatedBinPath {
				if err := os.Rename(unzippedPath, updatedBinPath); err != nil {
					log.Warnf("rename to updated path failed: %v", err)
					return fmt.Errorf("rename updated binary: %w", err)
				}
			}
		}
	}

	log.Infof("update core success")
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
		// Format: "<hash>  <filename>" or "<hash> <filename>"
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
	// Containers are now supported: binary is written to the data volume.
	// Only Windows is unsupported.
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
			return "octopus-linux-x86.zip", nil
		case "amd64":
			return "octopus-linux-x86_64.zip", nil
		case "arm":
			return "octopus-linux-armv7.zip", nil
		case "arm64":
			return "octopus-linux-arm64.zip", nil
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
