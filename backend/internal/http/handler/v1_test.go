package handler

import (
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestImageGenerationBodyAcceptsSingleImageURL(t *testing.T) {
	const referenceURL = "https://assets.example.test/reference.webp"
	var body imageGenerationBody
	if err := json.Unmarshal([]byte(`{"model":"gpt-image-2","image":"`+referenceURL+`"}`), &body); err != nil {
		t.Fatalf("unmarshal single image URL: %v", err)
	}
	if got, want := []string(body.Image), []string{referenceURL}; !reflect.DeepEqual(got, want) {
		t.Fatalf("image = %#v, want %#v", got, want)
	}
}

func TestImageGenerationBodyAcceptsImageURLArray(t *testing.T) {
	var body imageGenerationBody
	if err := json.Unmarshal([]byte(`{"image":["https://assets.example.test/one.webp","https://assets.example.test/two.webp"]}`), &body); err != nil {
		t.Fatalf("unmarshal image URL array: %v", err)
	}
	want := []string{"https://assets.example.test/one.webp", "https://assets.example.test/two.webp"}
	if got := []string(body.Image); !reflect.DeepEqual(got, want) {
		t.Fatalf("image = %#v, want %#v", got, want)
	}
}

func TestImageGenerationBodyRejectsInvalidImageShape(t *testing.T) {
	var body imageGenerationBody
	if err := json.Unmarshal([]byte(`{"image":{"url":"https://assets.example.test/reference.webp"}}`), &body); err == nil {
		t.Fatal("unmarshal object image unexpectedly succeeded")
	}
}

func TestRequestCallMetadata(t *testing.T) {
	cases := []struct {
		name       string
		path       string
		host       string
		proto      string
		portHeader string
		wantMethod string
		wantPort   int
	}{
		{
			name:       "api with explicit port",
			path:       "/v1/images/generations",
			host:       "image2api.example:2080",
			wantMethod: "API /v1",
			wantPort:   2080,
		},
		{
			name:       "playground behind https proxy",
			path:       "/admin/api/generate",
			host:       "image2api.example",
			proto:      "https",
			wantMethod: "画图台 /admin/api/generate",
			wantPort:   443,
		},
		{
			name:       "admin test proxy port takes precedence",
			path:       "/admin/api/test",
			host:       "image2api.example",
			portHeader: "8443, 443",
			wantMethod: "后台测试 /admin/api/test",
			wantPort:   8443,
		},
	}

	gin.SetMode(gin.TestMode)
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			req := httptest.NewRequest("POST", "http://"+tt.host+tt.path, nil)
			req.Host = tt.host
			req.Header.Set("X-Forwarded-Proto", tt.proto)
			req.Header.Set("X-Forwarded-Port", tt.portHeader)
			ctx.Request = req

			if got := requestCallMethod(ctx); got != tt.wantMethod {
				t.Fatalf("requestCallMethod() = %q, want %q", got, tt.wantMethod)
			}
			if got := requestPort(ctx); got != tt.wantPort {
				t.Fatalf("requestPort() = %d, want %d", got, tt.wantPort)
			}
		})
	}
}
