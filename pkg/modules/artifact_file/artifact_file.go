package artifact_file

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"

	"github.com/kubesphere/kubekey/v4/pkg/modules/internal"
	"github.com/kubesphere/kubekey/v4/pkg/utils/artifactpath"
	"github.com/kubesphere/kubekey/v4/pkg/variable"
)

// Run performs local artifact operations that otherwise require a Unix shell.
func Run(_ context.Context, opts internal.ExecOptions) (string, string, error) {
	if runtime.GOOS != "windows" || opts.Host != "localhost" {
		return internal.StdoutFailed, "", fmt.Errorf("artifact_file requires a Windows localhost")
	}
	vars, err := opts.GetAllVariables()
	if err != nil {
		return internal.StdoutFailed, internal.StderrGetHostVariable, err
	}
	args := variable.Extension2Variables(opts.Args)
	action, err := variable.StringVar(vars, args, "action")
	if err != nil {
		return internal.StdoutFailed, internal.StderrParseArgument, err
	}
	directory, err := variable.StringVar(vars, args, "directory")
	if err != nil || !filepath.IsAbs(directory) {
		return internal.StdoutFailed, "", fmt.Errorf("artifact directory must be an absolute path")
	}

	staging := filepath.Join(directory, "kubekey")
	switch action {
	case "prepare":
		if err = os.RemoveAll(staging); err == nil {
			err = os.MkdirAll(staging, 0o755)
		}
	case "pack":
		err = pack(staging, filepath.Join(directory, "kubekey-artifact.tgz"))
	case "unpack":
		archive, parseErr := variable.StringVar(vars, args, "archive")
		if parseErr != nil || !filepath.IsAbs(archive) {
			return internal.StdoutFailed, "", fmt.Errorf("artifact archive must be an absolute path")
		}
		err = unpack(archive, directory)
	default:
		err = fmt.Errorf("unknown artifact_file action %q", action)
	}
	if err != nil {
		return internal.StdoutFailed, "", err
	}
	return internal.StdoutSuccess, "", nil
}

func pack(dir, archive string) error {
	file, err := os.CreateTemp(filepath.Dir(archive), ".artifact-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	compressed := gzip.NewWriter(file)
	writer := tar.NewWriter(compressed)
	err = filepath.WalkDir(dir, func(filename string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if filename == dir {
			return nil
		}
		name, err := filepath.Rel(dir, filename)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported artifact entry %q", filename)
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = artifactpath.ArchiveName(filepath.ToSlash(name))
		if info.IsDir() {
			header.Mode = 0o755
		} else {
			header.Mode = 0o644
		}
		if err := writer.WriteHeader(header); err != nil || info.IsDir() {
			return err
		}
		source, err := os.Open(filename)
		if err != nil {
			return err
		}
		_, err = io.Copy(writer, source)
		return errors.Join(err, source.Close())
	})
	if err = errors.Join(err, writer.Close(), compressed.Close(), file.Close()); err != nil {
		return err
	}
	if err := os.Rename(file.Name(), archive); err != nil {
		return err
	}
	return os.RemoveAll(dir)
}
