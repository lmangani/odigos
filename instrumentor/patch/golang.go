package patch

import (
	"fmt"
	odigosv1 "github.com/keyval-dev/odigos/api/odigos/v1alpha1"
	"github.com/keyval-dev/odigos/common"
	"github.com/keyval-dev/odigos/common/consts"
	v1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	golangKernelDebugVolumeName = "kernel-debug"
	golangKernelDebugHostPath   = "/sys/kernel/debug"
	golangAgentName             = "keyval/otel-go-agent:v0.6.1"
	golangExporterEndpoint      = "OTEL_EXPORTER_OTLP_ENDPOINT"
	golangServiceNameEnv        = "OTEL_SERVICE_NAME"
	golangTargetExeEnv          = "OTEL_TARGET_EXE"
	initImage                   = "ghcr.io/keyval-dev/odigos/init:v0.1.37"
	initVolumeName              = "odigos"
	initMountPath               = "/odigos"
	initExePath                 = "/odigos/init"
)

var golang = &golangPatcher{}

type golangPatcher struct{}

func (g *golangPatcher) Patch(podSpec *v1.PodTemplateSpec, instrumentation *odigosv1.InstrumentedApplication) {
	modifiedContainers := podSpec.Spec.Containers

	podSpec.Spec.InitContainers = append(podSpec.Spec.InitContainers, v1.Container{
		Name:            "odigos-init",
		Image:           initImage,
		ImagePullPolicy: "IfNotPresent",
		Command:         []string{"cp", "-a", "/odigos-init/.", "/odigos/"},
		VolumeMounts: []v1.VolumeMount{
			{
				Name:      initVolumeName,
				MountPath: initMountPath,
			},
		},
	})

	for _, l := range instrumentation.Spec.Languages {
		if shouldPatch(instrumentation, common.GoProgrammingLanguage, l.ContainerName) {
			if l.ProcessName == "" {
				ctrl.Log.V(0).Info("could not find binary path for golang application",
					"container", l.ContainerName)
				continue
			}

			appName := l.ContainerName
			if len(instrumentation.Spec.Languages) == 1 && len(instrumentation.OwnerReferences) > 0 {
				appName = instrumentation.OwnerReferences[0].Name
			}
			bpfContainer := v1.Container{
				Name:  fmt.Sprintf("%s-instrumentation", l.ContainerName),
				Image: golangAgentName,
				Env: []v1.EnvVar{
					{
						Name: NodeIPEnvName,
						ValueFrom: &v1.EnvVarSource{
							FieldRef: &v1.ObjectFieldSelector{
								FieldPath: "status.hostIP",
							},
						},
					},
					{
						Name:  golangExporterEndpoint,
						Value: fmt.Sprintf("%s:%d", HostIPEnvValue, consts.OTLPPort),
					},
					{
						Name:  golangServiceNameEnv,
						Value: appName,
					},
					{
						Name:  golangTargetExeEnv,
						Value: l.ProcessName,
					},
				},
				VolumeMounts: []v1.VolumeMount{
					{
						Name:      golangKernelDebugVolumeName,
						MountPath: golangKernelDebugHostPath,
					},
				},
				SecurityContext: &v1.SecurityContext{
					Capabilities: &v1.Capabilities{
						Add: []v1.Capability{
							"SYS_PTRACE",
						},
					},
					Privileged: boolPtr(true),
					RunAsUser:  intPtr(0),
				},
			}

			for i, c := range modifiedContainers {
				if c.Name == l.ContainerName {
					targetC := &modifiedContainers[i]
					newArgs := calculateInitArgs(targetC.Command, targetC.Args, l.ProcessName)
					targetC.Command = []string{initExePath}
					targetC.Args = newArgs
					targetC.VolumeMounts = append(c.VolumeMounts,
						v1.VolumeMount{
							Name:      initVolumeName,
							MountPath: initMountPath,
						},
					)
					targetC.Env = append(c.Env,
						v1.EnvVar{
							Name: "HOST_IP",
							ValueFrom: &v1.EnvVarSource{
								FieldRef: &v1.ObjectFieldSelector{
									FieldPath: "status.hostIP",
								},
							},
						},
						v1.EnvVar{
							Name: "POD_NAME",
							ValueFrom: &v1.EnvVarSource{
								FieldRef: &v1.ObjectFieldSelector{
									FieldPath: "metadata.name",
								},
							},
						},
						v1.EnvVar{
							Name: "POD_NAMESPACE",
							ValueFrom: &v1.EnvVarSource{
								FieldRef: &v1.ObjectFieldSelector{
									FieldPath: "metadata.namespace",
								},
							},
						})
				}
			}

			modifiedContainers = append(modifiedContainers, bpfContainer)
		}
	}

	podSpec.Spec.Containers = modifiedContainers
	// TODO: if explicitly set to false, fallback to hostPID
	podSpec.Spec.ShareProcessNamespace = boolPtr(true)

	podSpec.Spec.Volumes = append(podSpec.Spec.Volumes, v1.Volume{
		Name: golangKernelDebugVolumeName,
		VolumeSource: v1.VolumeSource{
			HostPath: &v1.HostPathVolumeSource{
				Path: golangKernelDebugHostPath,
			},
		},
	},
		v1.Volume{
			Name: initVolumeName,
			VolumeSource: v1.VolumeSource{
				EmptyDir: &v1.EmptyDirVolumeSource{},
			},
		})
}

func (g *golangPatcher) IsInstrumented(podSpec *v1.PodTemplateSpec, instrumentation *odigosv1.InstrumentedApplication) bool {
	// TODO: Deep comparison
	for _, l := range instrumentation.Spec.Languages {
		if l.Language == common.GoProgrammingLanguage {
			for _, c := range podSpec.Spec.Containers {
				if c.Name == fmt.Sprintf("%s-instrumentation", l.ContainerName) {
					return true
				}
			}
		}
	}

	return false
}

func calculateInitArgs(origCommand []string, origArgs []string, exeFile string) []string {
	args := []string{exeFile}
	if len(origCommand) > 0 {
		args = append(args, origCommand...)
	}

	if len(origArgs) > 0 {
		// If args are specified, but no command we assume the running command is exePath
		// TODO: use CRI to figure out what is the real entrypoint
		if len(origCommand) == 0 {
			args = append(args, exeFile)
		}

		args = append(args, origArgs...)
	}

	return args
}

func boolPtr(b bool) *bool {
	return &b
}

func intPtr(n int64) *int64 {
	return &n
}
