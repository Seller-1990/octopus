package op

import (
	"net/http"

	"github.com/bestruirui/octopus/internal/apperror"
)

const (
	CodeSiteChannelAccountNotFound         = "site_channel.account_not_found"
	CodeSiteChannelRouteUpdateFailed       = "site_channel.route_update_failed"
	CodeSiteChannelModelDisableFailed      = "site_channel.model_disable_failed"
	CodeSiteChannelKeyCreateFailed         = "site_channel.key_create_failed"
	CodeSiteChannelSourceKeyUpdateFailed   = "site_channel.source_key_update_failed"
	CodeSiteChannelProjectedSettingsFailed = "site_channel.projected_settings_failed"
	CodeSiteChannelManualModelFailed       = "site_channel.manual_model_failed"
	CodeSiteChannelProjectFailed           = "site_channel.project_failed"
)

func newSiteChannelAccountNotFoundError() *apperror.Error {
	return apperror.New(CodeSiteChannelAccountNotFound, "site account not found").WithStatus(http.StatusNotFound)
}
