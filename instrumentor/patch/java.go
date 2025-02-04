package patch

import (
	"fmt"
	odigosv1 "github.com/keyval-dev/odigos/api/odigos/v1alpha1"
	"github.com/keyval-dev/odigos/common"
	"github.com/keyval-dev/odigos/common/consts"
	v1 "k8s.io/api/core/v1"
)

const (
	javaAgentImage               = "keyval/otel-java-agent:v0.4"
	javaVolumeName               = "agentdir-java"
	javaMountPath                = "/agent"
	otelResourceAttributesEnvVar = "OTEL_RESOURCE_ATTRIBUTES"
	otelResourceAttrPatteern     = "service.name=%s,k8s.pod.name=%s"
	javaOptsEnvVar               = "JAVA_OPTS"
	javaToolOptionsEnvVar        = "JAVA_TOOL_OPTIONS"
	javaToolOptionsPattern       = "-javaagent:/agent/opentelemetry-javaagent-all.jar " +
		"-Dotel.traces.sampler=always_on -Dotel.exporter.otlp.endpoint=http://%s:%d"
)

var java = &javaPatcher{}

type javaPatcher struct{}

func (j *javaPatcher) Patch(podSpec *v1.PodTemplateSpec, instrumentation *odigosv1.InstrumentedApplication) {
	podSpec.Spec.Volumes = append(podSpec.Spec.Volumes, v1.Volume{
		Name: javaVolumeName,
		VolumeSource: v1.VolumeSource{
			EmptyDir: &v1.EmptyDirVolumeSource{},
		},
	})

	podSpec.Spec.InitContainers = append(podSpec.Spec.InitContainers, v1.Container{
		Name:    "copy-java-agent",
		Image:   javaAgentImage,
		Command: []string{"cp", "/javaagent.jar", "/agent/opentelemetry-javaagent-all.jar"},
		VolumeMounts: []v1.VolumeMount{
			{
				Name:      javaVolumeName,
				MountPath: javaMountPath,
			},
		},
	})

	var modifiedContainers []v1.Container
	for _, container := range podSpec.Spec.Containers {
		if shouldPatch(instrumentation, common.JavaProgrammingLanguage, container.Name) {
			container.Env = append([]v1.EnvVar{{
				Name: NodeIPEnvName,
				ValueFrom: &v1.EnvVarSource{
					FieldRef: &v1.ObjectFieldSelector{
						FieldPath: "status.hostIP",
					},
				},
			},
				{
					Name: PodNameEnvVName,
					ValueFrom: &v1.EnvVarSource{
						FieldRef: &v1.ObjectFieldSelector{
							FieldPath: "metadata.name",
						},
					},
				},
			}, container.Env...)

			idx := getIndexOfEnv(container.Env, javaToolOptionsEnvVar)
			if idx == -1 {
				container.Env = append(container.Env, v1.EnvVar{
					Name:  javaToolOptionsEnvVar,
					Value: fmt.Sprintf(javaToolOptionsPattern, HostIPEnvValue, consts.OTLPPort),
				})
			} else {
				container.Env[idx].Value = container.Env[idx].Value + " " + fmt.Sprintf(javaToolOptionsPattern, HostIPEnvValue, consts.OTLPPort)
			}

			container.Env = append(container.Env, v1.EnvVar{
				Name:  javaOptsEnvVar,
				Value: fmt.Sprintf(javaToolOptionsPattern, HostIPEnvValue, consts.OTLPPort),
			})

			container.Env = append(container.Env, v1.EnvVar{
				Name:  otelResourceAttributesEnvVar,
				Value: fmt.Sprintf(otelResourceAttrPatteern, calculateAppName(podSpec, &container, instrumentation), PodNameEnvValue),
			})

			container.VolumeMounts = append(container.VolumeMounts, v1.VolumeMount{
				MountPath: javaMountPath,
				Name:      javaVolumeName,
			})
		}

		modifiedContainers = append(modifiedContainers, container)
	}

	podSpec.Spec.Containers = modifiedContainers
}

func (j *javaPatcher) IsInstrumented(podSpec *v1.PodTemplateSpec, instrumentation *odigosv1.InstrumentedApplication) bool {
	// TODO: Deep comparison
	for _, c := range podSpec.Spec.InitContainers {
		if c.Name == "copy-java-agent" {
			return true
		}
	}
	return false
}
