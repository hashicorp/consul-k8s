package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
)

const (
	Group               = "api-gateway.consul.hashicorp.com"
	annotationConfigKey = "api-gateway.consul.hashicorp.com/config"
)

var ErrUnmarshallingGatewayClassConfig = errors.New("error unmarshalling GatewayClassConfig annotation, skipping")

func SerializeGatewayClassConfig(ctx context.Context, client client.Client, gw *gwv1beta1.Gateway, gwc *gwv1beta1.GatewayClass) (bool, error) {
	var (
		config    v1alpha1.GatewayClassConfig
		err       error
		annotated bool
		managed   bool
	)

	if gw.Annotations == nil {
		gw.Annotations = make(map[string]string)
	}

	if annotatedConfig, ok := gw.Annotations[annotationConfigKey]; ok {
		if err := json.Unmarshal([]byte(annotatedConfig), &config.Spec); err != nil {
			return false, ErrUnmarshallingGatewayClassConfig
		}
		annotated = true
	}

	// check if we own the gateway
	config, managed, err = getConfigForGatewayClass(ctx, client, gwc)
	if err != nil {
		gw.Annotations[annotationConfigKey] = ""
		if k8serrors.IsNotFound(err) {
			// invalid config which means an invalid gatewayclass
			// so pretend we don't exist
			return false, nil
		}
		return false, err
	}

	if !managed {
		fmt.Printf("gw: %p\n", gw)
		gw.Annotations[annotationConfigKey] = ""
		// we don't own this gateway so we pretend it doesn't exist
		return false, nil
	}

	marshaled, err := json.Marshal(config.Spec)
	if err != nil {
		return false, err
	}

	gw.Annotations[annotationConfigKey] = string(marshaled)

	if !annotated {
		// we annotated for the first time
		return true, client.Update(ctx, gw)
	}

	return false, nil
}

func getConfigForGatewayClass(ctx context.Context, client client.Client, gwc *gwv1beta1.GatewayClass) (config v1alpha1.GatewayClassConfig, managed bool, err error) {
	fmt.Println("HERE")
	if ref := gwc.Spec.ParametersRef; ref != nil {
		if string(ref.Group) != Group || ref.Kind != v1alpha1.GatewayClassConfigKind {
			// pretend we have nothing because we don't support an untyped configuration
			return config, false, nil
		}

		err := client.Get(ctx, types.NamespacedName{Name: ref.Name}, &config)
		if err != nil {
			return config, false, err
		}

	}
	return config, true, nil
}
