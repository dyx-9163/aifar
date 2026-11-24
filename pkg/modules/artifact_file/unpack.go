package artifact_file

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"k8s.io/apimachinery/pkg/util/rand"

	"github.com/kubesphere/kubekey/v4/pkg/utils/artifactpath"
)

func unpack(archive, directory string) error {
	file, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer file.Close()
	compressed, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer compressed.Close()
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return err
	}
	defer root.Close()
	reader := tar.NewReader(compressed)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			// The tar end marker can precede the gzip checksum.
			_, err = io.Copy(io.Discard, compressed)
			return err
		}
		if err != nil {
			return err
		}
		if err := extractEntry(root, header, reader); err != nil {
			return fmt.Errorf("extract %q: %w", header.Name, err)
		}
	}
}

func extractEntry(root *os.Root, header *tar.Header, reader io.Reader) error {
	if header.Typeflag == tar.TypeDir && (header.Name == "." || header.Name == "./") {
		return nil
	}
	name := strings.TrimSuffix(strings.TrimPrefix(header.Name, "./"), "/")
	if name == "." || !fs.ValidPath(name) || strings.ContainsRune(name, '\\') {
		return fmt.Errorf("invalid artifact entry %q", header.Name)
	}
	if strings.HasPrefix(name, "images/") {
		name = artifactpath.ImagePath(name)
	}
	localName, err := filepath.Localize(name)
	if err != nil {
		return fmt.Errorf("invalid artifact entry %q: %w", header.Name, err)
	}
	switch header.Typeflag {
	case tar.TypeDir:
		return root.MkdirAll(localName, 0o755)
	case tar.TypeReg:
		if err := root.MkdirAll(filepath.Dir(localName), 0o755); err != nil {
			return err
		}
		tempName := ".kk-artifact-" + rand.String(12)
		file, err := root.OpenFile(tempName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			return err
		}
		defer root.Remove(tempName)
		_, copyErr := io.Copy(file, reader)
		if err := errors.Join(copyErr, file.Close()); err != nil {
			return err
		}
		return root.Rename(tempName, localName)
	default:
		return fmt.Errorf("unsupported artifact entry type %d", header.Typeflag)
	}
}
