// SPDX-License-Identifier: AGPL-3.0-or-later
package api

import (
	"github.com/88250/gulu"
	"github.com/siyuan-note/siyuan/kernel/apicontract"
	"github.com/siyuan-note/siyuan/kernel/conf"
	"github.com/siyuan-note/siyuan/kernel/model"
	"github.com/siyuan-note/siyuan/kernel/util"
)

// Keep the local account implementation outside the changing upstream handler.
func sftpLocalCloudUser() apicontract.Response[*apicontract.SettingUser] {
	user := &conf.User{
		UserId:                          "0",
		UserName:                        "_",
		UserAvatarURL:                   "data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7",
		UserHomeBImgURL:                 "",
		UserTitles:                      []*conf.UserTitle{},
		UserIntro:                       "",
		UserNickname:                    "",
		UserCreateTime:                  "29991231 00:00:00",
		UserSiYuanProExpireTime:         -1,
		UserToken:                       "token",
		UserTokenExpireTime:             "32503593600",
		UserSiYuanRepoSize:              0,
		UserSiYuanPointExchangeRepoSize: 0,
		UserSiYuanAssetSize:             0,
		UserTrafficUpload:               0,
		UserTrafficDownload:             0,
		UserTrafficAPIGet:               0,
		UserTrafficAPIPut:               0,
		UserTrafficTime:                 0,
		UserSiYuanSubscriptionPlan:      0,
		UserSiYuanSubscriptionStatus:    0,
		UserSiYuanSubscriptionType:      1,
		UserSiYuanOneTimePayStatus:      1,
	}

	model.Conf.SetUser(user)
	data, _ := gulu.JSON.MarshalJSON(user)
	model.Conf.UserData = util.AESEncrypt(string(data))
	model.Conf.Save()

	return apicontract.Success(settingUserPayload(user))
}
