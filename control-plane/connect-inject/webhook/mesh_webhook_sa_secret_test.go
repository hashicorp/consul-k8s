// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package webhook

import (
	"context"
	"testing"

	logrtest "github.com/go-logr/logr/testr"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

func TestServiceAccountTokenSecretName(t *testing.T) {
	const ns = "default"
	tokenSecret := func(name, saName string) *corev1.Secret {
		return &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:        name,
				Namespace:   ns,
				Annotations: map[string]string{corev1.ServiceAccountNameKey: saName},
			},
			Type: corev1.SecretTypeServiceAccountToken,
		}
	}
	dockercfg := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "web-admin-dockercfg-abcde", Namespace: ns},
		Type:       corev1.SecretTypeDockercfg,
	}
	sa := func(refs ...string) *corev1.ServiceAccount {
		s := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "web-admin", Namespace: ns}}
		for _, r := range refs {
			s.Secrets = append(s.Secrets, corev1.ObjectReference{Name: r})
		}
		return s
	}

	cases := map[string]struct {
		sa      *corev1.ServiceAccount
		objects []runtime.Object
		want    string
	}{
		"linked token secret is used": {
			sa:      sa("web-admin-token-xyz"),
			objects: []runtime.Object{tokenSecret("web-admin-token-xyz", "web-admin")},
			want:    "web-admin-token-xyz",
		},
		"OpenShift: dockercfg linked first is skipped": {
			sa:      sa("web-admin-dockercfg-abcde", "web-admin-token-xyz"),
			objects: []runtime.Object{dockercfg, tokenSecret("web-admin-token-xyz", "web-admin")},
			want:    "web-admin-token-xyz",
		},
		"OpenShift 4.16+: only dockercfg linked returns no token secret": {
			sa:      sa("web-admin-dockercfg-abcde"),
			objects: []runtime.Object{dockercfg},
			want:    "",
		},
		"token secret bound to another service account is not used": {
			sa:      sa("other-token"),
			objects: []runtime.Object{tokenSecret("other-token", "someone-else")},
			want:    "",
		},
		"missing linked secret is skipped": {
			sa:   sa("does-not-exist"),
			want: "",
		},
		"no linked secrets": {
			sa:   sa(),
			want: "",
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			w := MeshWebhook{
				Log:       logrtest.New(t),
				Clientset: fake.NewSimpleClientset(c.objects...),
			}
			require.Equal(t, c.want, w.serviceAccountTokenSecretName(context.Background(), ns, c.sa))
		})
	}
}
