package handlers

import (
	"net/http"
	"strconv"

	"github.com/bestruirui/octopus/internal/apperror"
	"github.com/bestruirui/octopus/internal/server/resp"
	"github.com/gin-gonic/gin"
)

// parseIDParam 解析路径 ID 参数（非法或非正数一律 400）——包级唯一权威，
// 此前 27 处调用点各抄一遍 strconv.Atoi + InvalidParam（C250913-08）。
func parseIDParam(c *gin.Context, name string) (int, bool) {
	idStr := c.Param(name)
	id, err := strconv.Atoi(idStr)
	if err != nil || id <= 0 {
		resp.InvalidParam(c)
		return 0, false
	}
	return id, true
}

// crudError 是 handlers 包统一的 CRUD 失败包装（C250913-08：三个一字不差
// 的函数合并；错误码常量仍按资源域命名，见下方常量表）。
func crudError(code string, message string, err error) *apperror.Error {
	return apperror.Wrap(code, message, err).WithStatus(http.StatusInternalServerError)
}

const (
	codeChannelNotFound          = "channel.not_found"
	codeChannelCreateFailed      = "channel.create_failed"
	codeChannelUpdateFailed      = "channel.update_failed"
	codeChannelDeleteFailed      = "channel.delete_failed"
	codeChannelFetchModelsFailed = "channel.fetch_models_failed"

	codeGroupNotFound            = "group.not_found"
	codeGroupCreateFailed        = "group.create_failed"
	codeGroupUpdateFailed        = "group.update_failed"
	codeGroupDeleteFailed        = "group.delete_failed"
	codeGroupPinFailed           = "group.pin_failed"
	codeGroupApplyDefaultsFailed = "group.apply_defaults_failed"

	codeGroupPresetListFailed        = "group.preset.list_failed"
	codeGroupPresetCreateFailed      = "group.preset.create_failed"
	codeGroupPresetCreateBlankFailed = "group.preset.create_blank_failed"
	codeGroupPresetCloneFailed       = "group.preset.clone_failed"
	codeGroupPresetUpdateFailed      = "group.preset.update_failed"
	codeGroupPresetDeleteFailed      = "group.preset.delete_failed"
	codeGroupPresetActivateFailed    = "group.preset.activate_failed"

	codeModelPriceUpdateFailed = "model.price_update_failed"
	codeModelPriceDeleteFailed = "model.price_delete_failed"
	codeModelCreateFailed      = "model.create_failed"
	codeModelUpdateFailed      = "model.update_failed"
	codeModelDeleteFailed      = "model.delete_failed"
)
