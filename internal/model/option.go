/*
 * Copyright 2025 CloudWeGo Authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package model

import "net/http"

const (
	defaultHttpPort = "52538"
)

// HandlerMount represents a custom http.Handler to be mounted at a prefix path.
type HandlerMount struct {
	Prefix  string
	Handler http.Handler
}

type DevOpt struct {
	DevServerPort string
	GoTypes       []RegisteredType
	Handlers      []HandlerMount
}

type DevOption func(*DevOpt)

func NewDevOpt(opts []DevOption) *DevOpt {
	o := &DevOpt{
		DevServerPort: defaultHttpPort,
	}
	for _, opt := range opts {
		opt(o)
	}
	return o
}
