/*
Copyright 2020 The Tekton Authors

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

package v1beta1_test

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	v1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	"github.com/tektoncd/pipeline/pkg/apis/pipeline/v1beta1"
	"github.com/tektoncd/pipeline/test/diff"
)

func TestResolverConversion(t *testing.T) {
	tests := []struct {
		name string
		in   *v1beta1.ResolverRef
		want *v1.ResolverRef
	}{{
		name: "resolver unit test",
		in: &v1beta1.ResolverRef{
			Resolver: "test",
			Params: []v1beta1.Param{
				{Name: "url", Value: v1beta1.ParamValue{StringVal: "https://github.com/tektoncd/catalog.git", Type: "string"}},
				{Name: "revision", Value: v1beta1.ParamValue{StringVal: "v1beta1", Type: "string"}},
				{Name: "pathInRepo", Value: v1beta1.ParamValue{StringVal: "git/git-clone.yaml", Type: "string"}},
			},
		},
		want: &v1.ResolverRef{
			Resolver: "test",
			Params: []v1.Param{
				{Name: "url", Value: v1.ParamValue{StringVal: "https://github.com/tektoncd/catalog.git", Type: "string"}},
				{Name: "revision", Value: v1.ParamValue{StringVal: "v1beta1", Type: "string"}},
				{Name: "pathInRepo", Value: v1.ParamValue{StringVal: "git/git-clone.yaml", Type: "string"}},
			},
		},
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			ver := &v1.ResolverRef{}
			test.in.ConvertTo(ctx, ver)
			t.Logf("ConvertTo() = %#v", ver)

			got := &v1beta1.ResolverRef{}

			got.ConvertFrom(ctx, *test.want)
			t.Logf("ConvertFrom() = %#v", got)
			if d := cmp.Diff(test.in, got); d != "" {
				t.Errorf("roundtrip %s", diff.PrintWantGot(d))
			}
		})
	}
}
