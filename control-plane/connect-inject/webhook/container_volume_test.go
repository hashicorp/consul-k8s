package webhook

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
)

func Test_containerVolume(t *testing.T) {
	cases := []struct {
		Name      string
		IsWindows bool
		ExpVolume corev1.Volume
	}{
		{
			Name:      "windows",
			IsWindows: true,
			ExpVolume: corev1.Volume{
				Name: volumeName,
				VolumeSource: corev1.VolumeSource{
					EmptyDir: &corev1.EmptyDirVolumeSource{Medium: corev1.StorageMediumDefault},
				},
			},
		},
		{
			Name:      "linux",
			IsWindows: false,
			ExpVolume: corev1.Volume{
				Name: volumeName,
				VolumeSource: corev1.VolumeSource{
					EmptyDir: &corev1.EmptyDirVolumeSource{Medium: corev1.StorageMediumMemory},
				},
			},
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			var w MeshWebhook
			vol := w.containerVolume(tt.IsWindows)
			assert.Equal(t, tt.ExpVolume, vol)
		})
	}

}
