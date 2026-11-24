//go:build builtin

package project

import (
	"context"
	"testing"

	kkcorev1 "github.com/kubesphere/kubekey/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestBuiltinArtifactExportPlaybook(t *testing.T) {
	p, err := New(context.Background(), kkcorev1.Playbook{
		ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{kkcorev1.BuiltinsProjectAnnotation: ""}},
		Spec:       kkcorev1.PlaybookSpec{Playbook: "playbooks/artifact_export.yaml"},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.MarshalPlaybook(); err != nil {
		t.Fatal(err)
	}
}
