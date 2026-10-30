package api

import (
	"github.com/88250/gulu"
	"github.com/gin-gonic/gin"
	"github.com/siyuan-note/siyuan/kernel/conf"
	"github.com/siyuan-note/siyuan/kernel/model"
	"net/http"
)

func setSyncProviderSFTP(c *gin.Context) {
	ret := gulu.Ret.NewResult()
	defer c.JSON(http.StatusOK, ret)

	var request struct {
		SFTP *conf.SFTP `json:"sftp" binding:"required"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		ret.Code = -1
		ret.Msg = err.Error()
		ret.Data = map[string]any{"closeTimeout": 5000}
		return
	}

	if err := model.SetSyncProviderSFTP(request.SFTP); err != nil {
		ret.Code = -1
		ret.Msg = err.Error()
		ret.Data = map[string]any{"closeTimeout": 5000}
		return
	}

	ret.Data = map[string]any{
		"sftp": model.GetSFTPConfig(),
	}
}
