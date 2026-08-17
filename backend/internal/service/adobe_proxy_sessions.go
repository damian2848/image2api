package service

import (
	"context"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	// Three simultaneous tasks per sticky session keeps each exit inside the
	// requested 2-4 task range while spreading a 100-request burst over roughly
	// 34 residential exits.
	adobeProxyTasksPerSession = int64(3)
	adobeProxySequenceKey     = "seq:p:adobe:proxy-session:v1"
	adobeProxySequenceTTL     = 7 * 24 * time.Hour
)

var (
	adobeProxySIDPart = regexp.MustCompile(`(?i)(^|-)sid-[^-]+`)
	adobeProxyTTLPart = regexp.MustCompile(`(?i)(^|-)t-[0-9]+($|-)`)
)

func is1024ProxyHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	return host == "1024proxy.io" || strings.HasSuffix(host, ".1024proxy.io")
}

// proxyURLWithAdobeSession replaces only 1024Proxy's sticky-session component.
// Credentials, location filters, gateway, port, and sticky duration are kept
// unchanged. Other proxy providers pass through untouched because their
// username parameter grammar may be different.
func proxyURLWithAdobeSession(rawProxy, sessionID string) (string, bool) {
	rawProxy = strings.TrimSpace(rawProxy)
	sessionID = strings.TrimSpace(sessionID)
	if rawProxy == "" || sessionID == "" {
		return rawProxy, false
	}
	for _, char := range sessionID {
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') && (char < '0' || char > '9') {
			return rawProxy, false
		}
	}

	parsed, err := url.Parse(rawProxy)
	if err != nil || parsed.User == nil || !is1024ProxyHost(parsed.Hostname()) {
		return rawProxy, false
	}
	username := parsed.User.Username()
	if username == "" {
		return rawProxy, false
	}
	if adobeProxySIDPart.MatchString(username) {
		username = adobeProxySIDPart.ReplaceAllString(username, "${1}sid-"+sessionID)
	} else {
		username += "-sid-" + sessionID
		if !adobeProxyTTLPart.MatchString(username) {
			username += "-t-5"
		}
	}
	if password, ok := parsed.User.Password(); ok {
		parsed.User = url.UserPassword(username, password)
	} else {
		parsed.User = url.User(username)
	}
	return parsed.String(), true
}

func adobeProxySessionID(sequence int64) string {
	if sequence < 1 {
		sequence = 1
	}
	group := (sequence-1)/adobeProxyTasksPerSession + 1
	return "img" + strconv.FormatInt(group, 36)
}

// adobeProxyForTask allocates one sticky session to a task. Every three
// allocations intentionally share a SID; retries inside that task keep using
// the same URL. A Redis sequence coordinates multiple backend instances, with a
// local sequence as a fail-open fallback.
func (s *V1Service) adobeProxyForTask(ctx context.Context, rawProxy string) (string, string) {
	if _, supported := proxyURLWithAdobeSession(rawProxy, "probe"); !supported {
		return strings.TrimSpace(rawProxy), ""
	}
	sequence, ok := s.conc.NextSequence(ctx, adobeProxySequenceKey, adobeProxySequenceTTL)
	if !ok {
		sequence = int64(s.adobeProxySessionSeq.Add(1))
	}
	sessionID := adobeProxySessionID(sequence)
	proxyURL, ok := proxyURLWithAdobeSession(rawProxy, sessionID)
	if !ok {
		return strings.TrimSpace(rawProxy), ""
	}
	return proxyURL, sessionID
}

// adobeProxyForFreshSession advances past any unused positions in the current
// three-task group. This matters for low traffic: simply taking the next
// sequence after a transport failure could otherwise return the same bad SID.
func (s *V1Service) adobeProxyForFreshSession(ctx context.Context, rawProxy, currentSession string) (string, string) {
	for attempts := int64(0); attempts <= adobeProxyTasksPerSession; attempts++ {
		proxyURL, sessionID := s.adobeProxyForTask(ctx, rawProxy)
		if sessionID == "" || sessionID != currentSession {
			return proxyURL, sessionID
		}
	}
	return strings.TrimSpace(rawProxy), ""
}
