package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

var (
	GroupVersion  = schema.GroupVersion{Group: "fleet.harbor-satellite.io", Version: "v1alpha1"}
	SchemeBuilder = &scheme.Builder{GroupVersion: GroupVersion}
	Scheme        = runtime.NewScheme()
)

func init() { //nolint:gochecknoinits
	SchemeBuilder.Register(&Satellite{}, &SatelliteList{})
	if err := SchemeBuilder.AddToScheme(Scheme); err != nil {
		panic(err)
	}
}
