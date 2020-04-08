/*
Copyright 2018 The Kubernetes Authors.

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

package flag

import (
	"flag"
	"strconv"
)

func DefineOrLookupStringFlag(name string, value string, usage string) *string {
	f := flag.Lookup(name)
	if f != nil {
		v := f.Value.String()
		return &v
	}
	return flag.String(name, value, usage)
}

func DefineOrLookupBoolFlag(name string, value bool, usage string) *bool {
	f := flag.Lookup(name)
	if f != nil {
		v, _ := strconv.ParseBool(f.Value.String())
		return &v
	}
	return flag.Bool(name, value, usage)
}
