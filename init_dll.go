// Copyright 2021 xgfone
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"fmt"
	"os"
	"path/filepath"
	stdplugin "plugin"
	"strings"
)

var (
	middlewareDllDirEnvName = strings.ToUpper(appName) + "_MIDDLEWARE_DLL_DIRS"
	pluginDllDirEnvName     = strings.ToUpper(appName) + "_PLUGIN_DLL_DIRS"
	sdDllDirEnvName         = strings.ToUpper(appName) + "_SD_DLL_DIRS"
)

var (
	middlewareDLLs []dllInfo
	pluginDLLs     []dllInfo
	sdDLLs         []dllInfo
)

type dllInfo struct {
	*stdplugin.Plugin
	Path string
}

func init() {
	var err error
	for _, env := range os.Environ() {
		if index := strings.IndexByte(env, '='); index > 0 {
			switch env[:index] {
			case middlewareDllDirEnvName:
				middlewareDLLs, err = getDLLsFromDirs(env[index+1:])
			case pluginDllDirEnvName:
				pluginDLLs, err = getDLLsFromDirs(env[index+1:])
			case sdDllDirEnvName:
				sdDLLs, err = getDLLsFromDirs(env[index+1:])
			}

			if err != nil {
				panic(err)
			}
		}
	}
}

func getDLLsFromDirs(d string) (dlls []dllInfo, err error) {
	var dll *stdplugin.Plugin
	for _, dir := range strings.Split(d, ",") {
		if dir = strings.TrimSpace(dir); dir != "" {
			err = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
				if err == nil && strings.HasSuffix(info.Name(), ".so") {
					if dll, err = stdplugin.Open(path); err != nil {
						return fmt.Errorf("fail to open the dll '%s': %v", path, err)
					}
					dlls = append(dlls, dllInfo{Plugin: dll, Path: path})
				}
				return err
			})

			if err != nil {
				return
			}
		}
	}
	return
}
