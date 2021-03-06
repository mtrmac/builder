package main

import (
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/containers/storage/pkg/reexec"
	"github.com/spf13/cobra"

	"k8s.io/apiserver/pkg/util/logs"

	"github.com/openshift/library-go/pkg/serviceability"

	"github.com/openshift/builder/pkg/version"
)

func main() {
	if reexec.Init() {
		return
	}

	logs.InitLogs()
	defer logs.FlushLogs()
	defer serviceability.BehaviorOnPanic(os.Getenv("OPENSHIFT_ON_PANIC"), version.Get())()
	defer serviceability.Profile(os.Getenv("OPENSHIFT_PROFILE")).Stop()

	rand.Seed(time.Now().UTC().UnixNano())
	if len(os.Getenv("GOMAXPROCS")) == 0 {
		runtime.GOMAXPROCS(runtime.NumCPU())
	}

	_, err := os.Stat("/var/run/secrets/kubernetes.io/serviceaccount/ca.crt")
	if !os.IsNotExist(err) {
		err := Copy("/var/run/secrets/kubernetes.io/serviceaccount/ca.crt", "/etc/pki/tls/certs/cluster.crt")
		if err != nil {
			fmt.Printf("Error setting up cluster CA cert: %v", err)
			os.Exit(1)
		}
	}

	_, err = os.Stat("/var/run/secrets/kubernetes.io/serviceaccount/service-ca.crt")
	if !os.IsNotExist(err) {
		err = Copy("/var/run/secrets/kubernetes.io/serviceaccount/service-ca.crt", "/etc/pki/tls/certs/service.crt")
		if err != nil {
			fmt.Printf("Error setting up service CA cert: %v", err)
			os.Exit(1)
		}
	}
	basename := filepath.Base(os.Args[0])
	command := CommandFor(basename)
	if err := command.Execute(); err != nil {
		os.Exit(1)
	}
}

// Copy the src file to dst. Any existing file will be overwritten and will not
// copy file attributes.
func Copy(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	if err != nil {
		return err
	}
	return out.Close()
}

// CommandFor returns the appropriate command for this base name,
// or the OpenShift CLI command.
func CommandFor(basename string) *cobra.Command {
	var cmd *cobra.Command

	switch basename {
	case "openshift-sti-build":
		cmd = NewCommandS2IBuilder(basename)
	case "openshift-docker-build":
		cmd = NewCommandDockerBuilder(basename)
	case "openshift-git-clone":
		cmd = NewCommandGitClone(basename)
	case "openshift-manage-dockerfile":
		cmd = NewCommandManageDockerfile(basename)
	case "openshift-extract-image-content":
		cmd = NewCommandExtractImageContent(basename)
	default:
		fmt.Printf("unknown command name: %s\n", basename)
		os.Exit(1)
	}

	GLog(cmd.PersistentFlags())

	return cmd
}
