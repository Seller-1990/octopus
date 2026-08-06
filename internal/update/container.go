package update

import (
	"os"
	"strings"
)

// InContainer 检测当前进程是否运行在容器中。
// 容器环境下自动更新仍然可用：新二进制写入数据卷，
// 由 entrypoint.sh 在下次启动或 syscall.Exec 热切换时生效。
func InContainer() bool {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	for _, path := range []string{"/proc/self/cgroup", "/proc/1/cgroup"} {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		content := string(data)
		if strings.Contains(content, "docker") ||
			strings.Contains(content, "containerd") ||
			strings.Contains(content, "kubepods") {
			return true
		}
	}
	return false
}

// ContainerDataDir returns the data directory used for in-container updates.
// It respects OCTOPUS_DATA_DIR env var, defaulting to "data" (relative to CWD).
func ContainerDataDir() string {
	if dir := os.Getenv("OCTOPUS_DATA_DIR"); dir != "" {
		return dir
	}
	return "data"
}
