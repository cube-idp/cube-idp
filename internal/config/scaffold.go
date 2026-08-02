package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	v1alpha1 "github.com/cube-idp/cube-idp/api/config/v1alpha1"
)

// scaffoldTemplate is a fixed hand-written document, not a yaml.Marshal
// of a Config (which would emit creationTimestamp: null noise).
// spec.cluster present-and-empty means "default kind cluster" per the
// cluster contract, so a scaffolded config provisions out of the box.
const scaffoldTemplate = `apiVersion: %s
kind: Config
metadata:
  name: %s
spec:
  cluster: {}
`

// ScaffoldFile creates a new Config document at path with the given cube
// name. The rendered document runs through the standard load pipeline
// before anything is written (an invalid name surfaces as the usual
// coded validation error and leaves no file behind), and the file is
// created O_EXCL so an existing document is never clobbered, even in a
// race with the caller's existence check.
func ScaffoldFile(path, name string) error {
	doc := fmt.Sprintf(scaffoldTemplate, v1alpha1.GroupVersion.String(), name)
	if _, err := decode([]byte(doc)); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return newAlreadyExistsError(path)
		}
		return newScaffoldFailedError(path, err)
	}
	// O_EXCL creation means the file is ours: remove it on a failed
	// write/close, or a retry would hit "file exists" on a broken file.
	if _, err := f.Write([]byte(doc)); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return newScaffoldFailedError(path, err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return newScaffoldFailedError(path, err)
	}
	return nil
}
