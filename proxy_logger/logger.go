/*
Copyright (c) 2021 Memgraph Ltd. [https://memgraph.com]

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package proxy_logger

import (
	"fmt"
	"io"
	"log"

	"github.com/memgraph/bolt-proxy/bolt"
)

var (
	DebugLog *log.Logger
	InfoLog  *log.Logger
	WarnLog  *log.Logger
)

const (
	// max bytes to display in logs in debug mode
	MAX_BYTES int = 32
)

func SetUpInfoLog(out io.Writer) {
	InfoLog = log.New(out, "INFO: ", log.Ldate|log.Ltime|log.Lmsgprefix)
}

func SetUpWarnLog(out io.Writer) {
	WarnLog = log.New(out, "WARN: ", log.Ldate|log.Ltime|log.Lmsgprefix)
}

func SetUpDebugLog(out io.Writer) {
	DebugLog = log.New(out, "DEBUG: ", log.Ldate|log.Ltime|log.Lmsgprefix)
}

func LogMessage(who string, msg *bolt.Message) {
	if msg != nil {
		end := MAX_BYTES
		suffix := fmt.Sprintf("...+%d bytes", len(msg.Data))
		if len(msg.Data) < MAX_BYTES {
			end = len(msg.Data)
			suffix = ""
		}
		switch msg.T {
		case bolt.HelloMsg:
			// make sure we don't print the secrets in a Hello!
			DebugLog.Printf("[%s] <%s>: %#v\n\n", who, msg.T, msg.Data[:4])
		case bolt.BeginMsg, bolt.FailureMsg:
			DebugLog.Printf("[%s] <%s>: %#v\n%s\n", who, msg.T, msg.Data[:end], msg.Data)
		default:
			DebugLog.Printf("[%s] <%s>: %#v%s, last2:%#v\n", who, msg.T, msg.Data[:end], suffix, msg.Data[len(msg.Data)-2:])
		}
	} else {
		DebugLog.Print("Message is nil")
	}
}

func LogMessages(who string, messages []*bolt.Message) {
	for _, msg := range messages {
		LogMessage(who, msg)
	}
}
