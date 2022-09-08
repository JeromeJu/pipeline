package v1beta1

import (
	"context"

	v1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
)

const bundleAnnotationKey = "tekton.dev/v1beta1Bundle"

func (tr TaskRef) convertTo(ctx context.Context, sink *v1.TaskRef) {
	sink.Name = tr.Name
	sink.Kind = v1.TaskKind(tr.Kind)
	sink.APIVersion = tr.APIVersion
	new := v1.ResolverRef{}
	tr.ResolverRef.convertTo(ctx, &new)
	sink.ResolverRef = new
	tr.convertBundleToResolver(sink)
}

func (tr *TaskRef) convertFrom(ctx context.Context, source v1.TaskRef) {
	tr.Name = source.Name
	tr.Kind = TaskKind(source.Kind)
	tr.APIVersion = source.APIVersion
	new := ResolverRef{}
	new.convertFrom(ctx, source.ResolverRef)
	tr.ResolverRef = new
	tr.convertResolverToBundle(source)
}

// convertBundleToResolver converts v1beta1 bundle string to a remote reference with the bundle resolver in v1.
func (tr TaskRef) convertBundleToResolver(sink *v1.TaskRef) {
	if tr.Bundle != "" {
		sink.ResolverRef = v1.ResolverRef{
			Resolver: "bundles",
			Params: []v1.Param{{
				Name:  "bundle",
				Value: v1.ParamValue{StringVal: tr.Bundle},
			}, {
				Name:  "name",
				Value: v1.ParamValue{StringVal: tr.Name},
			}, {
				Name:  "kind",
				Value: v1.ParamValue{StringVal: tr.Name},
			}},
		}
	}
}

//
func (tr *TaskRef) convertResolverToBundle(source v1.TaskRef) {
	if source.ResolverRef.Resolver == "bundles" {
		for _, p := range source.Params {
			if p.Name == "bundle" {
				tr.Bundle = p.Value.StringVal
			}
			if p.Name == "name" {
				tr.Name = p.Value.StringVal
			}
		}
	}
}
