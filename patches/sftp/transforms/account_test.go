package main

import (
	"bytes"
	"strings"
	"testing"
)

const prefix = `package api
var getCloudUser = contractHandler(apicontract.GetCloudUser, func(c *gin.Context, request apicontract.SettingCloudUserRequest) apicontract.Response[*apicontract.SettingUser] `
const suffix = `, func(c *gin.Context) *apicontract.Response[*apicontract.SettingUser] {
 if model.IsAdminRoleContext(c) { return nil }
 ret := apicontract.Success((*apicontract.SettingUser)(nil))
 return &ret
})
var untouched = "leave this alone"
`

func TestBodyChangesAndFormattingPreserveContractAndAuthorization(t *testing.T) {
	for _, body := range []string{
		`{ user, err := model.RefreshUser(request.Token); return payload(user, err) }`,
		`{ user, err := model.GetCloudUser(request.Token, request.Cached); return payload(user, err) }`,
		"{ /* braces { } */\n s := `}`; if changed { return nested(s) }; return nil }",
	} {
		result, err := rewrite([]byte(prefix + body + suffix))
		if err != nil {
			t.Fatal(err)
		}
		want := prefix + "{\n\treturn sftpLocalCloudUser()\n}" + suffix
		if string(result) != want {
			t.Fatalf("unexpected rewrite: %s", result)
		}
		again, err := rewrite(result)
		if err != nil || !bytes.Equal(result, again) {
			t.Fatal("rewrite must be idempotent", err)
		}
	}
}

func TestUnknownContractsFailClosed(t *testing.T) {
	valid := prefix + `{ return nil }` + suffix
	for _, src := range []string{
		strings.ReplaceAll(valid, "getCloudUser", "otherHandler"),
		strings.ReplaceAll(valid, "apicontract.GetCloudUser", "apicontract.OtherContract"),
		strings.ReplaceAll(valid, "request apicontract.SettingCloudUserRequest", "request OtherRequest"),
		strings.ReplaceAll(valid, "contractHandler(", "unknownWrapper("),
		valid + strings.TrimPrefix(valid, "package api\n"),
		"package api\nvar getCloudUser = broken{",
	} {
		if _, err := rewrite([]byte(src)); err == nil {
			t.Fatalf("accepted unknown contract: %s", src)
		}
	}
}
