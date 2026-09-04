package op

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/bestruirui/octopus/internal/apperror"
	"github.com/bestruirui/octopus/internal/conf"
	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/utils/log"
	"gorm.io/gorm"
)

var (
	userCacheMu sync.RWMutex
	userCache   model.User
)

// UserInit 初始化管理用户（F01 修复：取消固定 admin/admin）。
// 空库时必须显式提供 bootstrap 密码（配置 OCTOPUS_BOOTSTRAP_PASSWORD），
// 未设置则拒绝启动——杜绝固定默认凭据被直接接管。
// 用户名固定 admin（首启），密码取自 bootstrap；密码绝不写入日志。
// P1 修复：区分「空库」与「DB 故障」（First 报错不一定是空库）；bootstrap 密码
// 有最小长度校验（与 UI 改密 6 位下限对齐），防 1 字符字典。
func UserInit() error {
	var existing model.User
	if err := db.GetDB().First(&existing).Error; err == nil {
		userCacheMu.Lock()
		userCache = existing
		userCacheMu.Unlock()
		return nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("failed to query admin user: %w", err)
	}
	password := strings.TrimSpace(conf.AppConfig.Bootstrap.Password)
	if password == "" {
		return fmt.Errorf("no admin user found and bootstrap password is not set: set OCTOPUS_BOOTSTRAP_PASSWORD (or config bootstrap.password) to initialize the first admin user")
	}
	if len(password) < 6 {
		return fmt.Errorf("bootstrap password must be at least 6 characters")
	}
	user := model.User{
		Username:     "admin",
		Password:     password,
		TokenVersion: 0,
	}
	if err := user.HashPassword(); err != nil {
		return err
	}
	if err := db.GetDB().Create(&user).Error; err != nil {
		return fmt.Errorf("failed to create admin user: %w", err)
	}
	userCacheMu.Lock()
	userCache = user
	userCacheMu.Unlock()
	// F01：不记录密码明文（原 log.Infof("initial user: admin,password: admin") 会进日志采集系统）
	log.Infof("initial admin user created; please change the password after first login")
	return nil
}

func UserChangePassword(oldPassword, newPassword string) error {
	userCacheMu.Lock()
	defer userCacheMu.Unlock()
	if err := userCache.ComparePassword(oldPassword); err != nil {
		return apperror.Wrap(apperror.CodeAuthPasswordIncorrect, "incorrect old password", err).WithStatus(http.StatusBadRequest)
	}
	// 与 bootstrap 初始化同标准（user.go:35），防止把口令清空后以空口令重新登录
	if len(newPassword) < 6 {
		return apperror.New(apperror.CodeCommonInvalidParam, "new password must be at least 6 characters").WithStatus(http.StatusBadRequest)
	}

	next := userCache
	next.Password = newPassword
	if err := next.HashPassword(); err != nil {
		return fmt.Errorf("failed to hash new password: %w", err)
	}
	next.TokenVersion = userCache.TokenVersion + 1

	// 密码与 token 版本在同一 UPDATE 内原子落库；版本递增使全部存量
	// JWT 立即失效（签发时内嵌 ver claim，校验时比对当前版本）。
	if err := db.GetDB().Model(&model.User{}).
		Where("id = ?", next.ID).
		Updates(map[string]interface{}{
			"password":      next.Password,
			"token_version": next.TokenVersion,
		}).Error; err != nil {
		return fmt.Errorf("failed to update password: %w", err)
	}
	userCache = next
	return nil
}

func UserChangeUsername(newUsername string) error {
	userCacheMu.Lock()
	defer userCacheMu.Unlock()
	if userCache.Username == newUsername {
		return fmt.Errorf("new username is the same as the old username")
	}
	next := userCache
	next.Username = newUsername
	next.TokenVersion = userCache.TokenVersion + 1
	if err := db.GetDB().Model(&model.User{}).
		Where("id = ?", next.ID).
		Updates(map[string]interface{}{
			"username":      next.Username,
			"token_version": next.TokenVersion,
		}).Error; err != nil {
		return fmt.Errorf("failed to update username: %w", err)
	}
	userCache = next
	return nil
}

func UserVerify(username, password string) error {
	userCacheMu.RLock()
	defer userCacheMu.RUnlock()
	if username != userCache.Username {
		return fmt.Errorf("incorrect username")
	}
	if err := userCache.ComparePassword(password); err != nil {
		return fmt.Errorf("incorrect password")
	}
	return nil
}

// UserTokenVersion 返回当前用户的 token 版本（用于 JWT ver claim 校验）。
// 纯内存读取，不触 DB，可安全地在每次鉴权时调用。
func UserTokenVersion() int {
	userCacheMu.RLock()
	defer userCacheMu.RUnlock()
	return userCache.TokenVersion
}

func UserGet() model.User {
	userCacheMu.RLock()
	defer userCacheMu.RUnlock()
	return userCache
}
