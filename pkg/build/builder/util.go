package builder

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"math"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"

	docker "github.com/fsouza/go-dockerclient"
	s2iapi "github.com/openshift/source-to-image/pkg/api"
	s2iutil "github.com/openshift/source-to-image/pkg/util"

	buildapiv1 "github.com/openshift/api/build/v1"
	"github.com/openshift/builder/pkg/build/builder/cmd/dockercfg"
	builderutil "github.com/openshift/builder/pkg/build/builder/util"
)

var (
	// procCGroupPattern is a regular expression that parses the entries in /proc/self/cgroup
	procCGroupPattern = regexp.MustCompile(`\d+:([a-z_,]+):/.*/(\w+-|)([a-z0-9]+).*`)

	// ClientTypeUnknown is an error returned when we can't figure out
	// which type of "client" we're using.
	ClientTypeUnknown = errors.New("internal error: method not implemented for this client type")
)

// MergeEnv will take an existing environment and merge it with a new set of
// variables. For variables with the same name in both, only the one in the
// new environment will be kept.
func MergeEnv(oldEnv, newEnv []string) []string {
	key := func(e string) string {
		i := strings.Index(e, "=")
		if i == -1 {
			return e
		}
		return e[:i]
	}
	result := []string{}
	newVars := map[string]struct{}{}
	for _, e := range newEnv {
		newVars[key(e)] = struct{}{}
	}
	result = append(result, newEnv...)
	for _, e := range oldEnv {
		if _, exists := newVars[key(e)]; exists {
			continue
		}
		result = append(result, e)
	}
	return result
}

func reportPushFailure(err error, authPresent bool, pushAuthConfig docker.AuthConfiguration) error {
	// write extended error message to assist in problem resolution
	if authPresent {
		glog.V(0).Infof("Registry server Address: %s", pushAuthConfig.ServerAddress)
		glog.V(0).Infof("Registry server User Name: %s", pushAuthConfig.Username)
		glog.V(0).Infof("Registry server Email: %s", pushAuthConfig.Email)
		passwordPresent := "<<empty>>"
		if len(pushAuthConfig.Password) > 0 {
			passwordPresent = "<<non-empty>>"
		}
		glog.V(0).Infof("Registry server Password: %s", passwordPresent)
	}
	return fmt.Errorf("Failed to push image: %v", err)
}

// addBuildLabels adds some common image labels describing the build that produced
// this image.
func addBuildLabels(labels map[string]string, build *buildapiv1.Build) {
	labels[builderutil.DefaultDockerLabelNamespace+"build.name"] = build.Name
	labels[builderutil.DefaultDockerLabelNamespace+"build.namespace"] = build.Namespace
}

// readInt64 reads a file containing a 64 bit integer value
// and returns the value as an int64.  If the file contains
// a value larger than an int64, it returns MaxInt64,
// if the value is smaller than an int64, it returns MinInt64.
func readInt64(filePath string) (int64, error) {
	data, err := ioutil.ReadFile(filePath)
	if err != nil {
		return -1, err
	}
	s := strings.TrimSpace(string(data))
	val, err := strconv.ParseInt(s, 10, 64)
	// overflow errors are ok, we'll get return a math.MaxInt64 value which is more
	// than enough anyway.  For underflow we'll return MinInt64 and the error.
	if err != nil && err.(*strconv.NumError).Err == strconv.ErrRange {
		if s[0] == '-' {
			return math.MinInt64, err
		}
		return math.MaxInt64, nil
	} else if err != nil {
		return -1, err
	}
	return val, nil
}

// readNetClsCGroup parses /proc/self/cgroup in order to determine the container id that can be used
// the network namespace that this process is running on, it returns the cgroup and container type
// (docker vs crio).
func readNetClsCGroup(reader io.Reader) (string, string) {

	containerType := "docker"

	cgroups := make(map[string]string)
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		if match := procCGroupPattern.FindStringSubmatch(scanner.Text()); match != nil {
			containerType = strings.TrimSuffix(match[2], "-")

			list := strings.Split(match[1], ",")
			containerId := match[3]
			if len(list) > 0 {
				for _, key := range list {
					cgroups[key] = containerId
				}
			} else {
				cgroups[match[1]] = containerId
			}
		}
	}

	names := []string{"net_cls", "cpu"}
	for _, group := range names {
		if value, ok := cgroups[group]; ok {
			return value, containerType
		}
	}

	return "", containerType
}

// extractParentFromCgroupMap finds the cgroup parent in the cgroup map
func extractParentFromCgroupMap(cgMap map[string]string) (string, error) {
	memory, ok := cgMap["memory"]
	if !ok {
		return "", fmt.Errorf("could not find memory cgroup subsystem in map %v", cgMap)
	}
	glog.V(6).Infof("cgroup memory subsystem value: %s", memory)

	parts := strings.Split(memory, "/")
	if len(parts) < 2 {
		return "", fmt.Errorf("unprocessable cgroup memory value: %s", memory)
	}

	var cgroupParent string
	if strings.HasSuffix(memory, ".scope") {
		// systemd system, take the second to last segment.
		cgroupParent = parts[len(parts)-2]
	} else {
		// non-systemd, take everything except the last segment.
		cgroupParent = strings.Join(parts[:len(parts)-1], "/")
	}
	glog.V(5).Infof("found cgroup parent %v", cgroupParent)
	return cgroupParent, nil
}

// SafeForLoggingEnvironmentList returns a copy of an s2i EnvironmentList array with
// proxy credential values redacted.
func SafeForLoggingEnvironmentList(env s2iapi.EnvironmentList) s2iapi.EnvironmentList {
	newEnv := make(s2iapi.EnvironmentList, len(env))
	copy(newEnv, env)
	proxyRegex := regexp.MustCompile("(?i)proxy")
	for i, env := range newEnv {
		if proxyRegex.MatchString(env.Name) {
			newEnv[i].Value, _ = s2iutil.SafeForLoggingURL(env.Value)
		}
	}
	return newEnv
}

// SafeForLoggingS2IConfig returns a copy of an s2i Config with
// proxy credentials redacted.
func SafeForLoggingS2IConfig(config *s2iapi.Config) *s2iapi.Config {
	newConfig := *config
	newConfig.Environment = SafeForLoggingEnvironmentList(config.Environment)
	if config.ScriptDownloadProxyConfig != nil {
		newProxy := *config.ScriptDownloadProxyConfig
		newConfig.ScriptDownloadProxyConfig = &newProxy
		if newConfig.ScriptDownloadProxyConfig.HTTPProxy != nil {
			newConfig.ScriptDownloadProxyConfig.HTTPProxy = builderutil.SafeForLoggingURL(newConfig.ScriptDownloadProxyConfig.HTTPProxy)
		}

		if newConfig.ScriptDownloadProxyConfig.HTTPProxy != nil {
			newConfig.ScriptDownloadProxyConfig.HTTPSProxy = builderutil.SafeForLoggingURL(newConfig.ScriptDownloadProxyConfig.HTTPProxy)
		}
	}
	newConfig.ScriptsURL, _ = s2iutil.SafeForLoggingURL(newConfig.ScriptsURL)
	return &newConfig
}

// GetDockerAuthConfiguration provides a Docker authentication configuration when the
// PullSecret is specified.
func GetDockerAuthConfiguration(path string) (*docker.AuthConfigurations, error) {
	glog.V(2).Infof("Checking for Docker config file for %s in path %s", dockercfg.PullAuthType, path)
	dockercfgPath := dockercfg.GetDockercfgFile(path)
	if len(dockercfgPath) == 0 {
		return nil, fmt.Errorf("no docker config file found in '%s'", os.Getenv(dockercfg.PullAuthType))
	}
	glog.V(2).Infof("Using Docker config file %s", dockercfgPath)
	r, err := os.Open(dockercfgPath)
	if err != nil {
		return nil, fmt.Errorf("'%s': %s", dockercfgPath, err)
	}
	return docker.NewAuthConfigurations(r)
}

// ReadLines reads the content of the given file into a string slice
func ReadLines(fileName string) ([]string, error) {
	file, err := os.Open(fileName)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines, scanner.Err()
}

// ParseProxyURL parses a proxy URL and allows fallback to non-URLs like
// myproxy:80 (for example) which url.Parse no longer accepts in Go 1.8.  The
// logic is copied from net/http.ProxyFromEnvironment to try to maintain
// backwards compatibility.
func ParseProxyURL(proxy string) (*url.URL, error) {
	proxyURL, err := url.Parse(proxy)

	// logic copied from net/http.ProxyFromEnvironment
	if err != nil || !strings.HasPrefix(proxyURL.Scheme, "http") {
		// proxy was bogus. Try prepending "http://" to it and see if that
		// parses correctly. If not, we fall through and complain about the
		// original one.
		if proxyURL, err := url.Parse("http://" + proxy); err == nil {
			return proxyURL, nil
		}
	}

	return proxyURL, err
}
