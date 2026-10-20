package api

import (
	"github.com/88250/gulu"
	"github.com/gin-gonic/gin"
	"github.com/siyuan-note/siyuan/kernel/conf"
	"github.com/siyuan-note/siyuan/kernel/model"
	"github.com/siyuan-note/siyuan/kernel/util"
	"net/http"
)

func setSyncProviderSFTP(c *gin.Context) {
	ret := gulu.Ret.NewResult()
	defer c.JSON(http.StatusOK, ret)

	arg, ok := util.JsonArg(c, ret)
	if !ok {
		return
	}

	var sftpArg map[string]any
	if !util.ParseJsonArgs(arg, ret, util.BindJsonArg("sftp", &sftpArg, true, false)) {
		return
	}
	data, err := gulu.JSON.MarshalJSON(sftpArg)
	if err != nil {
		ret.Code = -1
		ret.Msg = err.Error()
		ret.Data = map[string]any{"closeTimeout": 5000}
		return
	}

	sftp := &conf.SFTP{}
	if err = gulu.JSON.UnmarshalJSON(data, sftp); err != nil {
		ret.Code = -1
		ret.Msg = err.Error()
		ret.Data = map[string]any{"closeTimeout": 5000}
		return
	}

	err = model.SetSyncProviderSFTP(sftp)
	if err != nil {
		ret.Code = -1
		ret.Msg = err.Error()
		ret.Data = map[string]any{"closeTimeout": 5000}
		return
	}

	ret.Data = map[string]any{
		"sftp": model.GetSFTPConfig(),
	}
}
