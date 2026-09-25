package handlers

import (
	"net/http"
	"strconv"

	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/server/middleware"
	"github.com/bestruirui/octopus/internal/server/resp"
	"github.com/bestruirui/octopus/internal/server/router"
	"github.com/gin-gonic/gin"
)

func init() {
	router.NewGroupRouter("/api/v1/model").
		Use(middleware.Auth()).
		AddRoute(
			router.NewRoute("/pricing/compare", http.MethodGet).
				Handle(compareModelPricing),
		)
}

// compareModelPricing GET /api/v1/model/pricing/compare?model=&days=7&limit=200
// 同一模型在不同站点账号间的价格横向对比（只读展示，不参与打分）。
func compareModelPricing(c *gin.Context) {
	modelName := c.Query("model")
	days, _ := strconv.Atoi(c.Query("days"))
	limit, _ := strconv.Atoi(c.Query("limit"))
	rows, summary, err := op.SiteModelPriceCompare(c.Request.Context(), modelName, days, limit)
	if err != nil {
		resp.InternalError(c)
		return
	}
	resp.Success(c, gin.H{
		"model":   modelName,
		"rows":    rows,
		"summary": summary,
	})
}
