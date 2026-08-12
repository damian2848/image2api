package handler

import (
	"encoding/json"
	"reflect"
	"testing"
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
