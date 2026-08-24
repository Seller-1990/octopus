package op

import (
	"context"
	"errors"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
)

func SiteChannelBindingGetByChannelID(channelID int, ctx context.Context) (*model.SiteChannelBinding, error) {
	bindings, err := allSiteChannelBindings(ctx)
	if err != nil {
		return nil, err
	}
	for i := range bindings {
		if bindings[i].ChannelID == channelID {
			binding := bindings[i]
			return &binding, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func SiteChannelBindingMapByChannelIDs(channelIDs []int, ctx context.Context) (map[int]model.SiteChannelBinding, error) {
	result := make(map[int]model.SiteChannelBinding)
	if len(channelIDs) == 0 {
		return result, nil
	}

	bindings, err := allSiteChannelBindings(ctx)
	if err != nil {
		return nil, err
	}
	for _, binding := range bindings {
		result[binding.ChannelID] = binding
	}
	return result, nil
}

// SiteChannelBindingListAll 返回全部站点投影渠道绑定，供 POR 任务按 siteAccountID 分组兄弟渠道。
func SiteChannelBindingListAll(ctx context.Context) ([]model.SiteChannelBinding, error) {
	var bindings []model.SiteChannelBinding
	err := db.GetDB().WithContext(ctx).Find(&bindings).Error
	return bindings, err
}

func ChannelManagedBinding(channelID int, ctx context.Context) (*model.SiteChannelBinding, bool, error) {
	binding, err := SiteChannelBindingGetByChannelID(channelID, ctx)
	if err == nil {
		return binding, true, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	return nil, false, err
}
