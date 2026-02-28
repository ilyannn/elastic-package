// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package lsp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
)

// jsonrpcMessage represents a JSON-RPC 2.0 message (request, notification, or response).
type jsonrpcMessage struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string           `json:"method,omitempty"`
	Params  json.RawMessage  `json:"params,omitempty"`
	Result  json.RawMessage  `json:"result,omitempty"`
	Error   *jsonrpcError    `json:"error,omitempty"`
}

type jsonrpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// JSON-RPC error codes.
const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInternalError  = -32603
	codeRequestFailed  = -32803
)

// readMessage reads one JSON-RPC message framed with Content-Length headers.
func readMessage(r *bufio.Reader) (*jsonrpcMessage, error) {
	contentLength := -1
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("reading header: %w", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		parts := strings.SplitN(line, ": ", 2)
		if len(parts) != 2 {
			continue
		}
		if strings.EqualFold(parts[0], "Content-Length") {
			n, err := strconv.Atoi(parts[1])
			if err != nil {
				return nil, fmt.Errorf("invalid Content-Length %q: %w", parts[1], err)
			}
			contentLength = n
		}
	}
	if contentLength < 0 {
		return nil, fmt.Errorf("missing Content-Length header")
	}

	body := make([]byte, contentLength)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, fmt.Errorf("reading body: %w", err)
	}

	var msg jsonrpcMessage
	if err := json.Unmarshal(body, &msg); err != nil {
		return nil, fmt.Errorf("decoding body: %w", err)
	}
	return &msg, nil
}

// messageWriter handles thread-safe writing of JSON-RPC messages to a writer.
type messageWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func newMessageWriter(w io.Writer) *messageWriter {
	return &messageWriter{w: w}
}

func (mw *messageWriter) write(v interface{}) error {
	body, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshaling message: %w", err)
	}
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(body))

	mw.mu.Lock()
	defer mw.mu.Unlock()
	if _, err := io.WriteString(mw.w, header); err != nil {
		return fmt.Errorf("writing header: %w", err)
	}
	if _, err := mw.w.Write(body); err != nil {
		return fmt.Errorf("writing body: %w", err)
	}
	return nil
}

func (mw *messageWriter) sendResponse(id *json.RawMessage, result interface{}) error {
	body, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("marshaling result: %w", err)
	}
	raw := json.RawMessage(body)
	return mw.write(&jsonrpcMessage{
		JSONRPC: "2.0",
		ID:      id,
		Result:  raw,
	})
}

func (mw *messageWriter) sendErrorResponse(id *json.RawMessage, code int, message string) error {
	return mw.write(&jsonrpcMessage{
		JSONRPC: "2.0",
		ID:      id,
		Error: &jsonrpcError{
			Code:    code,
			Message: message,
		},
	})
}

func (mw *messageWriter) sendNotification(method string, params interface{}) error {
	body, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("marshaling params: %w", err)
	}
	raw := json.RawMessage(body)
	return mw.write(&jsonrpcMessage{
		JSONRPC: "2.0",
		Method:  method,
		Params:  raw,
	})
}
