package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestSFTPConfigRequestValidation(t *testing.T) {
	for _, body := range []string{"", "{", "null", "{}", `{"sftp":null}`, `{"sftp":[]}`, `{"sftp":{"port":"wrong"}}`} {
		t.Run(body, func(t *testing.T) {
			writer := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(writer)
			c.Request = httptest.NewRequest(http.MethodPost, "/api/sync/setSyncProviderSFTP", strings.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")
			setSyncProviderSFTP(c)
			var response struct {
				Code int    `json:"code"`
				Msg  string `json:"msg"`
				Data struct {
					CloseTimeout int `json:"closeTimeout"`
				} `json:"data"`
			}
			if err := json.Unmarshal(writer.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if writer.Code != http.StatusOK || response.Code != -1 || response.Msg == "" || response.Data.CloseTimeout != 5000 {
				t.Fatalf("invalid SFTP input escaped validation: %s", writer.Body.String())
			}
		})
	}
}
