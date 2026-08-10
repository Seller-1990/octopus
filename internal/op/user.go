package op

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/bestruirui/octopus/internal/apperror"
	"github.com/bestruirui/octopus/internal/conf"
	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/utils/log"
	"gorm.io/gorm"
)

var userCache model.User

// UserInit 初始化管理用户（F01 修复：取消固定 admin/admin）。
// 空库时必须显式提供 bootstrap 密码（配置 OCTOPUS_BOOTSTRAP_PASSWORD），
// 未设置则拒绝启动——杜绝固定默认凭据被直接接管。
// 用户名固定 admin（首启），密码取自 bootstrap；密码绝不写入日志。
// P1 修复：区分「空库」与「DB 故障」（First 报错不一定是空库）；bootstrap 密码
// 有最小长度校验（与 UI 改密 6 位下限对齐），防 1 字符字典。
func UserInit() error {
	if err := db.GetDB().First(&userCache).Error; err == nil {
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
	userCache.Username = "admin"
	userCache.Password = password
	if err := userCache.HashPassword(); err != nil {
		return err
	}
	if err := db.GetDB().Create(&userCache).Error; err != nil {
		return fmt.Errorf("failed to create admin user: %w", err)
	}
	// F01：不记录密码明文（原 log.Infof("initial user: admin,password: admin") 会进日志采集系统）
	log.Infof("initial admin user created; please change the password after first login")
	return nil
}

func UserChangePassword(oldPassword, newPassword string) error {
	if err := userCache.ComparePassword(oldPassword); err != nil {
		return apperror.Wrap(apperror.CodeAuthPasswordIncorrect, "incorrect old password", err).WithStatus(http.StatusBadRequest)
	}

	userCache.Password = newPassword
	if err := userCache.HashPassword(); err != nil {
		return fmt.Errorf("failed to hash new password: %w", err)
	}

	if err := db.GetDB().Model(&userCache).Update("password", userCache.Password).Error; err != nil {
		return fmt.Errorf("failed to update password: %w", err)
	}

	return nil
}

func UserChangeUsername(newUsername string) error {
	if userCache.Username == newUsername {
		return fmt.Errorf("new username is the same as the old username")
	}
	userCache.Username = newUsername
	if err := db.GetDB().Model(&userCache).Update("username", userCache.Username).Error; err != nil {
		return fmt.Errorf("failed to update username: %w", err)
	}
	return nil
}

func UserVerify(username, password string) error {
	if username != userCache.Username {
		return fmt.Errorf("incorrect username")
	}
	if err := userCache.ComparePassword(password); err != nil {
		return fmt.Errorf("incorrect password")
	}
	return nil
}

func UserGet() model.User {
	return userCache
}
