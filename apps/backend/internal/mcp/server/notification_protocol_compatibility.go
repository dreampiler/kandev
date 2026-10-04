package mcp

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

// streamableHTTPHandler supplements missing notification metadata while leaving
// protocol validation and notification dispatch with the SDK.
func (s *Server) streamableHTTPHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		version := r.Header.Get(mcp.HeaderProtocolVersion)
		if r.Method != http.MethodPost || r.Body == nil ||
			!mcp.IsModernProtocol(version) || !s.httpServer.SupportsProtocolVersion(version) {
			s.httpServer.ServeHTTP(w, r)
			return
		}

		originalBody := r.Body
		defer func() { _ = originalBody.Close() }()
		body, err := io.ReadAll(r.Body)
		if err != nil {
			// Preserve the SDK's read-error response, including its transport checks.
			r.Body = io.NopCloser(notificationBodyReadError{err})
		} else {
			body = supplementNotificationProtocolVersion(body, version)
			r.Body = io.NopCloser(bytes.NewReader(body))
			r.ContentLength = int64(len(body))
		}
		s.httpServer.ServeHTTP(w, r)
	})
}

func supplementNotificationProtocolVersion(body []byte, version string) []byte {
	var message map[string]json.RawMessage
	if json.Unmarshal(body, &message) != nil || message == nil || !isNotificationEnvelope(body, message) {
		return body
	}
	params := make(map[string]json.RawMessage)
	if raw, present := message["params"]; present {
		if json.Unmarshal(raw, &params) != nil || params == nil {
			return body
		}
	}
	for key := range params {
		if strings.EqualFold(key, "_meta") {
			return body
		}
	}
	meta, err := json.Marshal(map[string]string{mcp.MetaKeyProtocolVersion: version})
	if err != nil {
		return body
	}
	params["_meta"] = meta
	message["params"], err = json.Marshal(params)
	if err != nil {
		return body
	}
	updated, err := json.Marshal(message)
	if err != nil {
		return body
	}
	return updated
}

func isNotificationEnvelope(body []byte, message map[string]json.RawMessage) bool {
	// The SDK's struct decoding also recognizes differently cased field names.
	// Leave those messages untouched rather than creating competing fields.
	for key := range message {
		switch strings.ToLower(key) {
		case "id":
			return false
		case "jsonrpc", "method", "params":
			if key != strings.ToLower(key) {
				return false
			}
		}
	}
	var envelope struct {
		JSONRPC string `json:"jsonrpc"`
		Method  string `json:"method"`
	}
	return json.Unmarshal(body, &envelope) == nil && envelope.JSONRPC == mcp.JSONRPC_VERSION && envelope.Method != ""
}

type notificationBodyReadError struct {
	err error
}

func (r notificationBodyReadError) Read([]byte) (int, error) {
	return 0, r.err
}
