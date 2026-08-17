package service

import (
	"context"
	"net/url"
	"strings"
	"sync"
	"testing"
)

func TestProxyURLWithAdobeSessionReplacesSID(t *testing.T) {
	raw := "http://customer-region-US-sid-old-t-5:p%40ss@us.1024proxy.io:3000"
	got, ok := proxyURLWithAdobeSession(raw, "img2")
	if !ok {
		t.Fatal("1024Proxy URL was not recognized")
	}
	parsed, err := url.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	if username := parsed.User.Username(); username != "customer-region-US-sid-img2-t-5" {
		t.Fatalf("username = %q", username)
	}
	if password, _ := parsed.User.Password(); password != "p@ss" {
		t.Fatalf("password was not preserved")
	}
}

func TestProxyURLWithAdobeSessionAddsStickyParameters(t *testing.T) {
	raw := "http://customer-region-US:secret@us.1024proxy.io:3000"
	got, ok := proxyURLWithAdobeSession(raw, "img1")
	if !ok {
		t.Fatal("1024Proxy URL was not recognized")
	}
	parsed, err := url.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	if username := parsed.User.Username(); username != "customer-region-US-sid-img1-t-5" {
		t.Fatalf("username = %q", username)
	}
}

func TestProxyURLWithAdobeSessionLeavesOtherProvidersUntouched(t *testing.T) {
	raw := "http://customer-sid-old:secret@proxy.example.com:3000"
	got, ok := proxyURLWithAdobeSession(raw, "img1")
	if ok || got != raw {
		t.Fatalf("non-1024 proxy changed: %q", got)
	}
}

func TestAdobeProxySessionIDGroupsThreeTasks(t *testing.T) {
	want := []string{"img1", "img1", "img1", "img2", "img2", "img2", "img3", "img3", "img3", "img4"}
	for index, expected := range want {
		if got := adobeProxySessionID(int64(index + 1)); got != expected {
			t.Fatalf("sequence %d = %q, want %q", index+1, got, expected)
		}
	}
}

func TestAdobeProxyFreshSessionSkipsRestOfFailedGroup(t *testing.T) {
	concurrency, _ := testConcurrencyService(t)
	v1 := &V1Service{conc: concurrency}
	baseProxy := "http://customer-region-US-sid-base-t-5:secret@us.1024proxy.io:3000"

	_, first := v1.adobeProxyForTask(context.Background(), baseProxy)
	if first != "img1" {
		t.Fatalf("first session = %q, want img1", first)
	}
	_, fresh := v1.adobeProxyForFreshSession(context.Background(), baseProxy, first)
	if fresh != "img2" {
		t.Fatalf("fresh session = %q, want img2", fresh)
	}
	_, next := v1.adobeProxyForTask(context.Background(), baseProxy)
	if next != "img2" {
		t.Fatalf("next grouped task = %q, want img2", next)
	}
}

func TestAdobeProxyConcurrentAllocationCapsEachSessionAtThree(t *testing.T) {
	concurrency, _ := testConcurrencyService(t)
	v1 := &V1Service{conc: concurrency}
	const requests = 100
	baseProxy := "http://customer-region-US-sid-base-t-5:secret@us.1024proxy.io:3000"

	sessions := make(chan string, requests)
	var group sync.WaitGroup
	for index := 0; index < requests; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			proxy, session := v1.adobeProxyForTask(context.Background(), baseProxy)
			if session == "" || !strings.Contains(proxy, "sid-"+session) {
				t.Errorf("invalid allocation: session=%q proxy=%q", session, proxy)
				return
			}
			sessions <- session
		}()
	}
	group.Wait()
	close(sessions)

	counts := map[string]int{}
	for session := range sessions {
		counts[session]++
	}
	if len(counts) != 34 {
		t.Fatalf("session count = %d, want 34", len(counts))
	}
	for session, count := range counts {
		if count < 1 || count > int(adobeProxyTasksPerSession) {
			t.Fatalf("session %s has %d tasks", session, count)
		}
	}
}
