// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package lsp

import (
	"fmt"
	"log"
	"time"
)

// logEvent writes a structured debug log entry to stderr.
// All fields are emitted as key=value pairs for easy parsing.
func logEvent(level string, fields map[string]interface{}) {
	msg := fmt.Sprintf("level=%s", level)
	msg += fmt.Sprintf(" ts=%s", time.Now().UTC().Format(time.RFC3339Nano))
	for k, v := range fields {
		msg += fmt.Sprintf(" %s=%v", k, v)
	}
	log.Println(msg)
}

func logDebug(method string, fields map[string]interface{}) {
	if fields == nil {
		fields = make(map[string]interface{})
	}
	fields["method"] = method
	logEvent("debug", fields)
}

func logInfo(method string, fields map[string]interface{}) {
	if fields == nil {
		fields = make(map[string]interface{})
	}
	fields["method"] = method
	logEvent("info", fields)
}

func logWarn(method string, fields map[string]interface{}) {
	if fields == nil {
		fields = make(map[string]interface{})
	}
	fields["method"] = method
	logEvent("warn", fields)
}

func logError(method string, fields map[string]interface{}) {
	if fields == nil {
		fields = make(map[string]interface{})
	}
	fields["method"] = method
	logEvent("error", fields)
}
