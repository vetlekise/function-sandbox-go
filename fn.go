package main

import (
	"context"

	"github.com/crossplane/function-sdk-go/errors"
	"github.com/crossplane/function-sdk-go/logging"
	fnv1 "github.com/crossplane/function-sdk-go/proto/v1"
	"github.com/crossplane/function-sdk-go/request"
	"github.com/crossplane/function-sdk-go/resource"
	"github.com/crossplane/function-sdk-go/resource/composed"
	"github.com/crossplane/function-sdk-go/response"
	"k8s.io/client-go/kubernetes/scheme"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Function returns whatever response you ask it to.
type Function struct {
	fnv1.UnimplementedFunctionRunnerServiceServer

	log logging.Logger
}

// RunFunction runs the Function.
func (f *Function) RunFunction(_ context.Context, req *fnv1.RunFunctionRequest) (*fnv1.RunFunctionResponse, error) {
	f.log.Info("Running function", "tag", req.GetMeta().GetTag())

	rsp := response.To(req, response.DefaultTTL)

	xr, err := request.GetObservedCompositeResource(req)
	if err != nil {
		response.Fatal(rsp, errors.Wrapf(err, "cannot get observed composite resource from %T", req))
		return rsp, nil
	}

	names, err := xr.Resource.GetStringArray("spec.environments")
	if err != nil {
		response.Fatal(rsp, errors.Wrapf(err, "cannot read spec.environments field of %s", xr.Resource.GetKind()))
		return rsp, nil
	}
	f.log.Info("Read environments from XR", "xr", xr.Resource.GetName(), "count", len(names), "environments", names)

	desired, err := request.GetDesiredComposedResources(req)
	if err != nil {
		response.Fatal(rsp, errors.Wrapf(err, "cannot get desired resources from %T", req))
		return rsp, nil
	}

	_ = scheme.AddToScheme(composed.Scheme) // registers all core k8s types

	for _, env := range names {
		f.log.Debug("Creating namespace for environment", "namespace", xr.Resource.GetName()+"-"+env)
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: xr.Resource.GetName() + "-" + env,
				Labels: map[string]string{
					"managed-by":  "crossplane",
					"environment": env,
				},
			},
		}

		cd, err := composed.From(ns)
		if err != nil {
			response.Fatal(rsp, errors.Wrapf(err, "cannot convert namespace %s", env))
			return rsp, nil
		}

		desired[resource.Name("ns-"+env)] = &resource.DesiredComposed{Resource: cd}
	}
	f.log.Info("Set desired namespaces", "count", len(desired))

	if err := response.SetDesiredComposedResources(rsp, desired); err != nil {
		response.Fatal(rsp, errors.Wrapf(err, "cannot set desired composed resources in %T", rsp))
		return rsp, nil
	}

	// You can set a custom status condition on the claim. This allows you to
	// communicate with the user. See the link below for status condition
	// guidance.
	// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties
	response.ConditionTrue(rsp, "FunctionSuccess", "Success").
		TargetCompositeAndClaim()

	return rsp, nil
}
