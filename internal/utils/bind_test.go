package utils

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

type bindTestRequest struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func setupBindTest() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/test", func(c *gin.Context) {
		var req bindTestRequest
		if !BindJSONOrError(c, "TEST", &req, "INVALID_REQUEST") {
			return
		}
		RespondSuccess(c, gin.H{"name": req.Name, "count": req.Count})
	})
	return r
}

func TestBindJSONOrErrorValid(t *testing.T) {
	r := setupBindTest()
	body := bytes.NewBufferString(`{"name":"alice","count":3}`)
	req := httptest.NewRequest(http.MethodPost, "/test", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"name":"alice"`)) {
		t.Errorf("response should contain bound name, got %s", w.Body.String())
	}
}

func TestBindJSONOrErrorInvalid(t *testing.T) {
	r := setupBindTest()
	body := bytes.NewBufferString(`{"name":`) // 非法 JSON
	req := httptest.NewRequest(http.MethodPost, "/test", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body=%s)", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"errorCode":"INVALID_REQUEST"`)) {
		t.Errorf("response should contain errorCode INVALID_REQUEST, got %s", w.Body.String())
	}
}

func TestBindJSONOrErrorWithoutContentType(t *testing.T) {
	r := setupBindTest()
	// gin 的 ShouldBindJSON 不强制 Content-Type：合法 JSON body 也能绑定成功
	body := bytes.NewBufferString(`{"name":"alice"}`)
	req := httptest.NewRequest(http.MethodPost, "/test", body)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (gin binds JSON without Content-Type, body=%s)", w.Code, w.Body.String())
	}
}

func TestBindJSONOrErrorEmptyBody(t *testing.T) {
	r := setupBindTest()
	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body=%s)", w.Code, w.Body.String())
	}
}
