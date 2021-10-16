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

package bolt

import (
	"bytes"
	"errors"
	"fmt"
)

// Mode of a Bolt Transaction for determining cluster routing
type Mode string

const (
	ReadMode  Mode = "READ"
	WriteMode      = "WRITE"
)

// Inspect bytes for valid Bolt Magic pattern, returning true if found.
// Otherwise, returns false with an error.
func ValidateMagic(magic []byte) (bool, error) {
	// 0x60, 0x60, 0xb0, 0x17
	if len(magic) < 4 {
		return false, errors.New("magic too short")
	}
	if bytes.Equal(magic, []byte{0x60, 0x60, 0xb0, 0x17}) {
		return true, nil
	}

	return false, errors.New("invalid magic bytes")
}

// Inspect client and server communication for valid Bolt handshake,
// returning the handshake value's bytes.
//
// In order to pick the version, we use a simple formula of:
//
//   min( max(client versions), max(server versions) )
//
// So if client_versions=[4.0, 3.5, 3.4]
// and server_versions=[4.2, 4.1, 4.0, 3.5]
// then min(max(4.0, 3.5, 3.4), max([4.2, 4.1, 4.0, 3.5])) ==>
//   min(4.0, 4.2) ==> 4.0!
//
// Though, in reality, right now we already have the max(serverVersion)
// as the server []byte argument.
//
// If the handshake is input bytes or handshake is invalid, returns
// an error and an empty byte array.
func ValidateHandshake(client, server []byte) ([]byte, error) {
	if len(server) != 4 {
		return nil, fmt.Errorf("server handshake wrong size is %v", len(server))
	}

	if len(client) != 16 {
		return nil, fmt.Errorf("client handshake wrong size %v", len(client))
	}

	chosen := make([]byte, 4)

	// find max(clientVersion)
	clientVersion := []byte{0x0, 0x0, 0x0, 0x0}
	for i := 0; i < 4; i++ {
		part := client[i*4 : i*4+4]
		if part[3] > clientVersion[3] ||
			(part[3] == clientVersion[3] && part[2] > clientVersion[2]) {
			clientVersion = part
		}
		// XXX: we assume nobody cares about patch level or lower
	}

	// our hacky min() logic
	if clientVersion[3] > server[3] {
		// differing major number, client newer
		copy(chosen, server)
	} else if clientVersion[3] == server[3] {
		// check minor numbers
		if clientVersion[2] > server[2] {
			copy(chosen, server)
		} else {
			copy(chosen, clientVersion)
		}
	} else {
		// client is older
		copy(chosen, clientVersion)
	}

	return chosen, nil
}

// Try to find and validate the Mode for some given bytes, returning
// the Mode if found or if valid looking Bolt chatter. Otherwise,
// returns nil and an error.
func ValidateMode(buf []byte) (Mode, error) {
	if IdentifyType(buf) == BeginMsg {
		tinyMap, _, err := ParseMap(buf[4:])
		if err != nil {
			panic(err)
		}

		value, found := tinyMap["mode"]
		if found {
			mode, ok := value.(string)
			if ok && mode == "r" {
				return ReadMode, nil
			}
		}
	}
	return WriteMode, nil
}
