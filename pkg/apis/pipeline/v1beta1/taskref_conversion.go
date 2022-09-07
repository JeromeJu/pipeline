package v1beta1

import (
	"context"

	v1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	"github.com/tektoncd/pipeline/pkg/apis/version"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const bundleAnnotationKey = "tekton.dev/v1beta1Bundle"

func (tr TaskRef) convertTo(ctx context.Context, sink *v1.TaskRef) {
	sink.Name = tr.Name
	sink.Kind = v1.TaskKind(tr.Kind)
	sink.APIVersion = tr.APIVersion
	new := v1.ResolverRef{}
	tr.ResolverRef.convertTo(ctx, &new)
	sink.ResolverRef = new
}

func (tr *TaskRef) convertFrom(ctx context.Context, source v1.TaskRef) {
	tr.Name = source.Name
	tr.Kind = TaskKind(source.Kind)
	tr.APIVersion = source.APIVersion
	new := ResolverRef{}
	new.convertFrom(ctx, source.ResolverRef)
	tr.ResolverRef = new
}

// convertBundle converts v1beta1 bundle string to a remote reference with the bundle resolver in v1.
func (tr *TaskRef) convertBundle() error {
	if tr.Bundle == "" {
		return nil
	}

	return version.SerializeToMetadata(meta, tr.Bundle, bundleAnnotationKey)
}

func deserializeBundle(meta *metav1.ObjectMeta, tr *TaskRef) error {
	bundle := ""
	err := version.DeserializeFromMetadata(meta, &bundle, bundleAnnotationKey)
	if err != nil {
		return err
	}
	tr.Bundle = bundle
	return nil
}
