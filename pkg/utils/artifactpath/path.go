package artifactpath

import (
	"runtime"
	"strings"
)

var encode = strings.NewReplacer("%", "%25", ":", "%3A")
var decode = strings.NewReplacer("%25", "%", "%3A", ":")

// ImagePath escapes OCI digests and registry ports for Windows file names.
func ImagePath(name string) string {
	if runtime.GOOS == "windows" {
		return encode.Replace(name)
	}
	return name
}

// ArchiveName preserves KubeKey's OCI layout when packaging Windows files.
func ArchiveName(name string) string {
	if runtime.GOOS == "windows" && strings.HasPrefix(name, "images/") {
		return decode.Replace(name)
	}
	return name
}
