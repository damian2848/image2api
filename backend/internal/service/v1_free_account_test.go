package service

import (
	"testing"

	"backend/internal/model"
	"gorm.io/datatypes"
)

func TestAdobeAccountCanServeModel(t *testing.T) {
	restricted := &model.ModelConfig{ID: "firefly-gpt-image-2", FreeAllowed: false}
	free := model.TokenAccount{Meta: datatypes.JSONMap{"plan": "free"}}
	member := model.TokenAccount{Meta: datatypes.JSONMap{"plan": "premium"}}

	if adobeAccountCanServeModel(free, restricted, "1K") {
		t.Fatal("a free account must not serve a restricted model by default")
	}
	free.FreeAllowed = true
	if !adobeAccountCanServeModel(free, restricted, "1K") {
		t.Fatal("the account-level override must allow a free account to serve a restricted model")
	}
	if !adobeAccountCanServeModel(member, restricted, "1K") {
		t.Fatal("member accounts must remain eligible for restricted models")
	}
}
