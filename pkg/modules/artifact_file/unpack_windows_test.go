package artifact_file

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/kubesphere/kubekey/v4/pkg/modules/internal"
	"github.com/kubesphere/kubekey/v4/pkg/modules/testutil"
)

func TestUnpackWindowsArtifact(t *testing.T) {
	for _, tt := range []struct {
		name       string
		entry      string
		outputName string
		wantError  bool
	}{
		{"OCI blob", "./images/blobs/sha256:abcd", "images/blobs/sha256%3Aabcd", false},
		{"ordinary file", "./manifests.yaml", "manifests.yaml", false},
		{"outside destination", "../escaped", "", true},
		{"Windows separator", `images\..\..\escaped`, "", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			archive := filepath.Join(dir, "offline package.tgz")
			destination := filepath.Join(dir, "binary directory")
			file, err := os.Create(archive)
			require.NoError(t, err)
			gz := gzip.NewWriter(file)
			writer := tar.NewWriter(gz)
			content := []byte("artifact payload")
			require.NoError(t, writer.WriteHeader(&tar.Header{
				Name: tt.entry, Mode: 0o644, Size: int64(len(content)),
			}))
			_, err = writer.Write(content)
			require.NoError(t, err)
			require.NoError(t, writer.Close())
			require.NoError(t, gz.Close())
			require.NoError(t, file.Close())

			raw, err := json.Marshal(map[string]string{
				"action": "unpack", "directory": destination, "archive": archive,
			})
			require.NoError(t, err)
			_, _, err = Run(t.Context(), internal.ExecOptions{
				Host: "localhost", Variable: testutil.NewTestVariable([]string{"localhost"}, nil),
				Args: runtime.RawExtension{Raw: raw},
			})
			if tt.wantError {
				require.ErrorContains(t, err, "invalid artifact entry")
				require.NoFileExists(t, filepath.Join(dir, "escaped"))
				return
			}
			require.NoError(t, err)
			got, err := os.ReadFile(filepath.Join(dir, "binary directory", filepath.FromSlash(tt.outputName)))
			require.NoError(t, err)
			require.Equal(t, content, got)
		})
	}
}
